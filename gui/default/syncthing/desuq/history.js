// desuqcafe fork: the history screen.
//
// Two questions that sound like one and are not:
//
//   "what has been happening in my folders"  -- who touched what, recently
//   "can I get the old one back"             -- the archive, thirty days deep
//
// They are different data with different lifetimes, so they are two tabs
// rather than one merged timeline. Recent changes come from /rest/events/disk,
// live in memory, and start again from nothing every time the daemon
// restarts. Older versions come from the staggered versioner on disk and
// survive everything. A single list would have to pretend those are the same
// kind of fact, and the place that pretence breaks is exactly where somebody
// is looking for a file they lost.
//
// WHAT THE CHANGES FEED CANNOT SAY, AND WHY THE COPY IS CAREFUL
//
// Four properties of /rest/events/disk that the wording here is built around,
// all verified rather than assumed:
//
//   - **action is only ever "modified" or "deleted"** (lib/model/folder.go).
//     A file that has just been *created* arrives as modified. So nothing in
//     this tab ever says "added" -- it says "changed", which is true of both.
//   - **IDs restart at 1 when the daemon restarts.** The buffer is memory
//     only. A stored `since` must be dropped the moment an id goes backwards
//     or the feed silently shows nothing forever. See resetIfRestarted.
//   - **The first scan emits one event per existing file**, one per file with
//     a single timestamp. On a real asset folder that overruns the thousand
//     deep buffer on its own, which is why runs are collapsed and why the
//     footer says plainly that this is not a complete record.
//   - **Ignored files emit nothing** -- emitDiskChangeEvents skips invalid
//     entries -- so a folder that has been through the picker is quieter here
//     than it is on disk.
//
// It is a fixed-mask endpoint, one server-side buffer subscribed at daemon
// start, so the per-subscription mask trap that applies to /rest/events does
// not apply here. Asking with a different mask is not possible; there is no
// mask.
//
// THE THIRD TAB: TWO PEOPLE EDITED THE SAME FILE
//
// Syncthing renames the losing copy to
// scene.sync-conflict-20260824-032916-F67Q3OS.blend and stops. The tray says
// so, and until now nothing could act on it. A .blend cannot be merged, so the
// only resolution is a choice between two whole files -- which means the
// screen's real job is showing which is which. Size, time, who wrote it, and
// for an image, the picture itself.
//
// The device id in a conflict file's name is NOT the author of that copy. See
// lib/api/api_conflicts.go: the id belongs to whoever's edit *won*. Both
// authors here come from the server, off the index, and neither is read out of
// the name.
//
// WHY THE ARCHIVE TAB TALKS TO THE FORK'S OWN ENDPOINT
//
// /rest/folder/history rather than upstream's /rest/folder/versions: the
// latter answers with every version of every file in one document, which for
// the folder this fork exists to serve is tens of megabytes to render twenty
// rows. See lib/api/api_history.go. Restoring still goes through upstream's
// POST, which was always the right shape.

angular.module('syncthing.core')

    .factory('desuqHistory', function ($http, $q, $window, $rootScope) {
        'use strict';

        // Only polled while the screen is open and on the changes tab.
        var POLL_MS = 3000;

        // How much of the tail to ask for when the screen opens. The server
        // buffer is a thousand deep; asking for all of it means a first scan
        // fills the screen with one row per file in the folder. Three hundred
        // is several days of ordinary work and about a screen and a half once
        // runs are collapsed.
        var BOOTSTRAP_LIMIT = 300;

        // Events within this many milliseconds of each other, by the same
        // person, in the same folder, doing the same thing, are one row.
        // Saving a .blend writes several files; a scan writes hundreds.
        var RUN_MS = 60000;

        var st = {
            open: false,
            tab: 'changes',
            // '' means every folder.
            folder: '',
            folders: [],
            search: '',

            // Changes tab.
            days: [],
            changesReady: false,
            changesError: '',

            // Versions tab.
            archive: { files: 0, versions: 0, bytes: 0, total: 0, rows: [] },
            // True when at least one folder in view keeps old copies at all.
            // A folder with versioning off has an empty archive for a reason
            // that is a setting rather than a lack of activity, and the two
            // read very differently to somebody hunting for a lost file.
            anyVersioning: false,
            expanded: {},
            versionsReady: false,
            versionsError: '',
            truncated: false,

            // Conflicts tab.
            conflicts: { total: 0, folders: [] },
            conflictsReady: false,
            conflictsError: '',
            // confirm is set when the server refuses a resolution because the
            // folder keeps no history, so the copy not kept would be deleted
            // for good. It holds everything needed to repeat the call with
            // force, and the screen turns into a question until it is answered.
            confirm: null,

            // Shared.
            busy: false,
            notice: ''
        };

        var timer = null;
        var lastID = 0;
        var events = [];
        var shortNames = {};
        var myShort = '';

        // ---------------------------------------------------------------- io

        function loadDevices() {
            return $q.all({
                cfg: $http.get(urlbase + '/config'),
                status: $http.get(urlbase + '/system/status')
            }).then(function (r) {
                shortNames = {};
                var devices = (r.cfg.data && r.cfg.data.devices) || [];
                devices.forEach(function (d) {
                    // modifiedBy on a disk event is the seven character short
                    // id, which is the leading run of the device id with its
                    // separators removed.
                    shortNames[shortOf(d.deviceID)] = d.name || shortOf(d.deviceID);
                });
                myShort = shortOf((r.status.data && r.status.data.myID) || '');

                st.folders = ((r.cfg.data && r.cfg.data.folders) || []).map(function (f) {
                    return { id: f.id, label: f.label || f.id };
                });
            });
        }

        function shortOf(id) {
            return String(id || '').replace(/-/g, '').slice(0, 7);
        }

        // who turns a modifiedBy short id into something a person recognises.
        // Our own id is "You" -- on a local change Syncthing stamps the file
        // with this device, so without this every row a modeller caused would
        // read as their own device name, which is technically right and reads
        // like somebody else did it.
        function who(short) {
            if (!short) {
                return 'Somebody';
            }
            if (short === myShort) {
                return 'You';
            }
            return shortNames[short] || short;
        }

        // ------------------------------------------------------- the changes

        function bootstrapChanges() {
            st.changesError = '';
            return $http.get(urlbase + '/events/disk', { params: { limit: BOOTSTRAP_LIMIT } })
                .then(function (r) {
                    events = r.data || [];
                    lastID = events.length ? events[events.length - 1].id : 0;
                    st.changesReady = true;
                    regroup();
                })
                .catch(function () {
                    st.changesError = 'Could not read recent changes.';
                    st.changesReady = true;
                });
        }

        function pollChanges() {
            return $http.get(urlbase + '/events/disk', { params: { since: lastID, timeout: 1 } })
                .then(function (r) {
                    var fresh = r.data || [];
                    if (!fresh.length) {
                        return checkRestart();
                    }
                    if (resetIfRestarted(fresh)) {
                        return bootstrapChanges();
                    }
                    events = events.concat(fresh);
                    // Keep only what the screen could plausibly show. The
                    // server buffer is the real limit; this stops a long
                    // session growing without bound.
                    if (events.length > BOOTSTRAP_LIMIT * 3) {
                        events = events.slice(events.length - BOOTSTRAP_LIMIT * 3);
                    }
                    lastID = events[events.length - 1].id;
                    regroup();
                })
                .catch(function () {
                    // A failed poll is not worth a message: the next one is
                    // three seconds away and the list on screen is still true.
                });
        }

        // A poll with since=4211 against a daemon that has restarted does NOT
        // hand back the new events numbered from 1. lib/events' Since() waits
        // for its counter to pass 4211 first -- so it returns nothing, every
        // time, until four thousand more things have happened. The feed just
        // stops, and looks exactly like a quiet afternoon.
        //
        // The only way to see a restart from here is to ask where the buffer
        // has got to. An empty poll is the cue, throttled, because an empty
        // poll is also what every genuinely quiet three seconds looks like.
        var RESTART_CHECK_MS = 15000;
        var restartChecked = 0;

        function checkRestart() {
            var now = Date.now();
            if (!lastID || now - restartChecked < RESTART_CHECK_MS) {
                return;
            }
            restartChecked = now;
            return $http.get(urlbase + '/events/disk', { params: { since: 0, limit: 1, timeout: 0 } })
                .then(function (r) {
                    var newest = (r.data || [])[0];
                    // Behind where we were, or nothing at all when we had
                    // something: either way this is a different buffer.
                    if (!newest || newest.id < lastID) {
                        events = [];
                        lastID = 0;
                        return bootstrapChanges();
                    }
                }, angular.noop);
        }

        // resetIfRestarted is the other half: a poll that does return events,
        // but not newer ones. The id is the only thing to go on, and one at or
        // below lastID is not progress.
        function resetIfRestarted(fresh) {
            if (fresh[0].id > lastID) {
                return false;
            }
            events = [];
            lastID = 0;
            return true;
        }

        // regroup turns the flat event list into days of collapsed rows.
        function regroup() {
            var wanted = events.filter(function (e) {
                var d = e.data || {};
                if (st.folder && d.folder !== st.folder) {
                    return false;
                }
                if (st.search && !matches(d.path || '', st.search)) {
                    return false;
                }
                // Who is working on what (lib/api/api_claims.go) is kept in
                // files, so marking one "changes" a file. That is bookkeeping,
                // and the main screen already says it in words.
                if (/^\.desuq-claims([\\/]|$)/.test(d.path || '')) {
                    return false;
                }
                // Directories being touched is plumbing, not news.
                return d.type !== 'dir';
            });

            // Newest first.
            wanted = wanted.slice().reverse();

            var rows = collapse(wanted);
            st.days = intoDays(rows);
        }

        // collapse folds a run of same-person, same-folder, same-action events
        // into one row. The threshold is a minute because that is roughly how
        // long a save takes to land and comfortably shorter than the gap
        // between two deliberate edits.
        function collapse(list) {
            var out = [];
            list.forEach(function (e) {
                var d = e.data || {};
                var at = new Date(e.time);
                var prev = out.length ? out[out.length - 1] : null;
                var sameRun = prev &&
                    prev.folder === d.folder &&
                    prev.by === d.modifiedBy &&
                    prev.action === d.action &&
                    Math.abs(prev.at - at) < RUN_MS;

                if (sameRun) {
                    prev.count++;
                    if (prev.names.length < 6) {
                        prev.names.push(pretty(d.path));
                    }
                    // A run is stamped with its newest member.
                    if (at > prev.at) {
                        prev.at = at;
                    }
                    return;
                }

                out.push({
                    folder: d.folder,
                    label: d.label || d.folder,
                    by: d.modifiedBy,
                    person: who(d.modifiedBy),
                    action: d.action,
                    at: at,
                    count: 1,
                    names: [pretty(d.path)]
                });
            });
            return out;
        }

        function intoDays(rows) {
            var days = [];
            var current = null;
            rows.forEach(function (r) {
                var key = dayKey(r.at);
                if (!current || current.key !== key) {
                    current = { key: key, title: dayTitle(r.at), rows: [] };
                    days.push(current);
                }
                current.rows.push(r);
            });
            return days;
        }

        // ------------------------------------------------------ the archive

        function loadArchive() {
            st.versionsError = '';
            var folders = st.folder ? [st.folder] : st.folders.map(function (f) { return f.id; });
            if (!folders.length) {
                st.archive = { files: 0, versions: 0, bytes: 0, total: 0, rows: [] };
                st.anyVersioning = false;
                st.versionsReady = true;
                return $q.when();
            }

            return $q.all(folders.map(function (id) {
                return $http.get(urlbase + '/folder/history', {
                    params: { folder: id, prefix: st.search }
                }).then(function (r) {
                    return r.data;
                }).catch(function () {
                    return null;
                });
            })).then(function (parts) {
                var merged = { files: 0, versions: 0, bytes: 0, total: 0, rows: [] };
                var failed = 0;
                var anyVersioning = false;
                parts.forEach(function (p, i) {
                    if (!p) {
                        failed++;
                        return;
                    }
                    if (p.versioning) {
                        anyVersioning = true;
                    }
                    merged.files += p.files;
                    merged.versions += p.versions;
                    merged.bytes += p.bytes;
                    merged.total += p.total;
                    (p.rows || []).forEach(function (row) {
                        row.folder = folders[i];
                        row.folderLabel = labelOf(folders[i]);
                        merged.rows.push(row);
                    });
                });

                // Merging several folders means re-sorting: each arrived
                // newest-first within itself, which says nothing about the
                // order between them.
                merged.rows.sort(function (a, b) {
                    return new Date(b.newest) - new Date(a.newest);
                });

                st.truncated = merged.rows.length < merged.total;
                st.anyVersioning = anyVersioning;
                st.archive = merged;
                st.versionsReady = true;
                st.versionsError = failed
                    ? 'Could not read the archive for ' + failed + ' of ' + parts.length + ' folders.'
                    : '';
            });
        }

        function labelOf(id) {
            for (var i = 0; i < st.folders.length; i++) {
                if (st.folders[i].id === id) {
                    return st.folders[i].label;
                }
            }
            return id;
        }

        function rowKey(row) {
            return row.folder + '\x00' + row.name;
        }

        function toggle(row) {
            var key = rowKey(row);
            if (st.expanded[key]) {
                delete st.expanded[key];
                return;
            }
            st.expanded[key] = { loading: true, versions: [], error: '' };
            $http.get(urlbase + '/folder/history', {
                params: { folder: row.folder, file: row.name }
            }).then(function (r) {
                st.expanded[key].versions = (r.data && r.data.versions) || [];
                st.expanded[key].loading = false;
            }).catch(function () {
                st.expanded[key].error = 'Could not read the older copies of this file.';
                st.expanded[key].loading = false;
            });
        }

        // restore puts one archived copy back.
        //
        // Syncthing's restore does not throw away what is there now: the
        // current file is itself archived first, so an accidental restore is
        // undone by restoring the copy this call just made. That is worth
        // knowing and the confirmation says it, because "restore" otherwise
        // sounds exactly like "overwrite and lose".
        function restore(row, version) {
            var body = {};
            body[row.name] = version.versionTime;
            st.busy = true;
            st.notice = '';
            return $http.post(urlbase + '/folder/versions?folder=' + encodeURIComponent(row.folder), body)
                .then(function (r) {
                    var err = r.data && r.data[row.name];
                    if (err) {
                        st.notice = 'Could not put back ' + pretty(row.name) + ': ' + err;
                        return;
                    }
                    st.notice = pretty(row.name) + ' has been put back. The copy that was there a moment ago is now the newest thing in this list.';
                    delete st.expanded[rowKey(row)];
                    return loadArchive();
                })
                .catch(function () {
                    st.notice = 'Could not put back ' + pretty(row.name) + '.';
                })
                .finally(function () {
                    st.busy = false;
                });
        }

        // ------------------------------------------------------- conflicts

        // loadConflicts asks for one folder or all of them, and applies the
        // search box on this side. The server has no filter and does not need
        // one: this is a list of things somebody has to decide about, and if
        // it is long enough for paging to matter the folder has a much bigger
        // problem than the screen does.
        function loadConflicts() {
            st.conflictsError = '';
            var params = {};
            if (st.folder) {
                params.folder = st.folder;
            }
            return $http.get(urlbase + '/folder/conflicts', { params: params })
                .then(function (r) {
                    var data = r.data || {};
                    var folders = (data.folders || []).map(function (f) {
                        var copy = angular.extend({}, f);
                        copy.rows = (f.rows || []).filter(function (row) {
                            return matches(row.name, st.search);
                        });
                        return copy;
                    }).filter(function (f) {
                        return f.rows.length;
                    });
                    st.conflicts = { total: data.total || 0, folders: folders };
                })
                .catch(function () {
                    st.conflictsError = 'Could not read the list of conflicting copies.';
                })
                .finally(function () {
                    st.conflictsReady = true;
                });
        }

        // resolveConflict performs one decision. keep is 'current' or 'aside',
        // spelled the way the screen is -- see api_conflicts.go for why "mine"
        // and "theirs" would be a lie about which copy is whose.
        function resolveConflict(folder, row, keep, force) {
            st.busy = true;
            st.notice = '';
            return $http.post(urlbase + '/folder/conflict', {
                folder: folder,
                conflict: row.conflict,
                keep: keep,
                force: !!force
            }).then(function (r) {
                st.confirm = null;
                st.notice = conflictNotice(row, keep, r.data || {});
                // The folder card carries the same count on a one-minute
                // probe, and it is the thing that sent most people here.
                $rootScope.$broadcast('desuq:conflictsChanged');
                return loadConflicts();
            }).catch(function (e) {
                // 409 with no versioning is a question, not a failure: the
                // server refused because one of the two copies would be gone
                // for good, and that is a thing to be asked rather than
                // discovered afterwards.
                if (e && e.status === 409 && String(e.data || '').indexOf('keeps no history') !== -1) {
                    st.confirm = { folder: folder, row: row, keep: keep };
                    return;
                }
                st.notice = 'Could not resolve ' + pretty(row.name) + ': ' +
                    String((e && e.data) || 'the folder may have changed since this list was drawn').trim();
            }).finally(function () {
                st.busy = false;
            });
        }

        // Plain text, no markup: st.notice is rendered through {{ }} like every
        // other fork string, so a <b> in here reaches the screen as four
        // literal characters. It did, once.
        function conflictNotice(row, keep, res) {
            var name = pretty(row.name);
            var other = res.archived
                ? 'The other one is under Older versions if you want it back.'
                : 'The other one has been deleted.';
            if (keep === 'aside') {
                return name + ': the copy that was set aside is now the one in use. ' + other;
            }
            return name + ': kept the copy that was already in use. ' + other;
        }

        function cancelConfirm() {
            st.confirm = null;
        }

        // --------------------------------------------------------- lifecycle

        function open(folderID, tab) {
            st.open = true;
            st.tab = tab || 'changes';
            st.folder = folderID || '';
            st.search = '';
            st.notice = '';
            st.expanded = {};
            st.changesReady = false;
            st.versionsReady = false;
            st.conflictsReady = false;
            st.confirm = null;

            loadDevices().finally(function () {
                refresh();
                start();
                // Asked for whatever tab is open, because the count is on the
                // tab itself: a screen that only mentions conflicts once you
                // have already thought to look for them is the situation this
                // tab exists to fix.
                if (st.tab !== 'conflicts') {
                    loadConflicts();
                }
            });
        }

        function close() {
            st.open = false;
            stop();
        }

        function refresh() {
            if (st.tab === 'conflicts') {
                return loadConflicts();
            }
            if (st.tab === 'changes') {
                if (!st.changesReady) {
                    return bootstrapChanges();
                }
                regroup();
                return $q.when();
            }
            return loadArchive();
        }

        function setTab(tab) {
            st.tab = tab;
            st.notice = '';
            st.confirm = null;
            if (tab === 'changes') {
                start();
                if (!st.changesReady) {
                    bootstrapChanges();
                }
                return;
            }
            stop();
            if (tab === 'conflicts') {
                loadConflicts();
                return;
            }
            loadArchive();
        }

        function setFolder(id) {
            st.folder = id;
            st.expanded = {};
            st.confirm = null;
            refresh();
            if (st.tab !== 'conflicts') {
                loadConflicts();
            }
        }

        function setSearch(text) {
            st.search = text;
            st.expanded = {};
            refresh();
        }

        function start() {
            stop();
            if (st.tab !== 'changes') {
                return;
            }
            timer = $window.setInterval(function () {
                if (!st.open || st.tab !== 'changes') {
                    return;
                }
                pollChanges();
            }, POLL_MS);
        }

        function stop() {
            if (timer) {
                $window.clearInterval(timer);
                timer = null;
            }
        }

        // Upstream's collapsed "Technical details" block lives in index.html at
        // controller scope, outside both directives, so it cannot see the child
        // scope this state is normally read through -- and it stayed on screen
        // underneath the history view. One property on $rootScope is cheaper
        // than a second service or an event.
        $rootScope.desuqHistoryState = st;

        // Explorer's "Show history" on a .blend (custom/tray/explorer.go)
        // opens the GUI at ?desuq-history=<folder>&file=<path>. That lands on
        // the versions tab, searched for the file's name: the name rather
        // than the path because disk events carry backslashes and the search
        // is a plain substring match, and a same-named file elsewhere in the
        // folder is a small price for never showing nothing.
        //
        // The parameters are taken off the address afterwards, so reloading
        // the page does not throw the screen open again over whatever the
        // person has moved on to.
        function openFromURL() {
            var q = {};
            String($window.location.search || '').replace(/^\?/, '').split('&').forEach(function (kv) {
                var i = kv.indexOf('=');
                if (i > 0) {
                    try {
                        q[decodeURIComponent(kv.slice(0, i))] = decodeURIComponent(kv.slice(i + 1).replace(/\+/g, ' '));
                    } catch (e) { /* a malformed escape is not ours */ }
                }
            });
            if (!q['desuq-history']) {
                return false;
            }
            open(q['desuq-history'], 'versions');
            // After open(), which clears the search; its first load is
            // asynchronous and reads st.search when it runs.
            st.search = pretty(q.file || '');
            if ($window.history && $window.history.replaceState) {
                $window.history.replaceState(null, '', $window.location.pathname);
            }
            return true;
        }
        openFromURL();

        return {
            state: st,
            open: open,
            close: close,
            setTab: setTab,
            setFolder: setFolder,
            setSearch: setSearch,
            toggle: toggle,
            expandedFor: function (row) { return st.expanded[rowKey(row)]; },
            restore: restore,
            refresh: refresh,
            resolveConflict: resolveConflict,
            cancelConfirm: cancelConfirm,
            loadConflicts: loadConflicts,
            // Exported for custom/scripts/test-history-render.js, which
            // asserts them directly rather than through the DOM.
            _collapse: collapse,
            _openFromURL: openFromURL,
            _intoDays: intoDays,
            _pollChanges: function () { return pollChanges(); },
            _lastID: function () { return lastID; },
            _resetIfRestarted: function (fresh, last) {
                lastID = last;
                events = [1, 2, 3];
                var did = resetIfRestarted(fresh);
                return { reset: did, lastID: lastID, events: events.length };
            }
        };
    })

    .directive('desuqHistory', function (desuqHistory) {
        'use strict';
        return {
            restrict: 'E',
            templateUrl: 'syncthing/desuq/historyView.html',
            scope: true,
            link: function (scope) {
                scope.hist = desuqHistory.state;
                scope.history = desuqHistory;
                // The template's two formatters. Not inherited: this directive
                // sits beside <desuq-home> rather than inside it, so its
                // parent is the controller scope, which has neither.
                scope.bytes = desuqBytes;
                scope.pretty = pretty;
                scope.previewable = previewable;
                scope.previewURL = previewURL;
                scope.prettyArchive = prettyArchive;
                scope.conflictOriginal = conflictOriginal;
            }
        };
    })
    // desuqThumb is one thumbnail, and the whole of its job is disappearing
    // quietly.
    //
    // /rest/folder/preview refuses anything it cannot decode -- a .png that is
    // really a .tga renamed, a file still being written, an archived copy the
    // versioner has since cleaned away -- and a bare <img> answers that with a
    // broken-image icon, which reads as "your file is damaged" rather than
    // "there is no picture of this". So the element removes itself instead.
    //
    // A directive rather than an inline onerror because the GUI is served with
    // its own headers and inline handlers are the kind of thing a future
    // Content-Security-Policy switches off silently.
    .directive('desuqThumb', function () {
        'use strict';
        return {
            restrict: 'E',
            scope: { src: '@' },
            template: '<img class="desuq-thumb" ng-show="ok" ng-src="{{ src }}" alt="" />',
            link: function (scope, el) {
                scope.ok = true;
                el.find('img').on('error', function () {
                    scope.$applyAsync(function () {
                        scope.ok = false;
                    });
                });
            }
        };
    });

// -------------------------------------------------------------- small helpers
//
// Deliberately at file scope and dependency-free so the render test can
// require this file and call them without an Angular injector.

// pretty turns an index path into the name a person would say. Disk events
// carry the OS separator -- filepath.FromSlash on the server -- so this has to
// cope with both.
function pretty(path) {
    var parts = String(path || '').split(/[\\\/]/);
    return parts[parts.length - 1] || String(path || '');
}

// desuqBytes is the same formatter home.js uses, deliberately copied rather
// than shared: it is twelve pure lines, and a shared module would put a load
// order between two files that otherwise have none.
function desuqBytes(bytes) {
    var b = Number(bytes) || 0;
    if (b < 1024) {
        return b + ' B';
    }
    var units = ['KiB', 'MiB', 'GiB', 'TiB'];
    var i = -1;
    do {
        b = b / 1024;
        i++;
    } while (b >= 1024 && i < units.length - 1);
    return (b >= 10 ? Math.round(b) : Math.round(b * 10) / 10) + ' ' + units[i];
}

// conflictOriginal is the file a conflict copy is a copy *of*, or '' for an
// ordinary name. The same surgery as lib/api/api_conflicts.go's conflictParse,
// and for the same reason: the extension sits after the stamp, so splitting on
// the last dot of the whole name gets an extensionless file wrong.
//
// This exists because resolving a conflict archives the copy that lost, and an
// archived copy keeps the name it had -- so the archive fills up with rows
// called texture4.sync-conflict-20260826-155856-V7OXBJ3.png. Verified on a live
// pair: that is exactly what .stversions holds afterwards. Somebody looking for
// the copy they did not keep is looking for "texture4.png".
function conflictOriginal(name) {
    var base = pretty(name);
    var i = base.lastIndexOf('.sync-conflict-');
    if (i < 0) {
        return '';
    }
    var rest = base.slice(i + '.sync-conflict-'.length);
    var dot = rest.lastIndexOf('.');
    return base.slice(0, i) + (dot > -1 ? rest.slice(dot) : '');
}

// prettyArchive is what an archive row is called on screen. The generated name
// is still shown underneath as the path, so nothing is hidden -- it is just
// not the headline.
function prettyArchive(name) {
    return conflictOriginal(name) || pretty(name);
}

// previewable is the same extension list lib/api/api_preview.go accepts.
// Checked here so an unpreviewable file costs no request at all -- a folder of
// .blend files would otherwise ask for, and be refused, one thumbnail per row.
function previewable(name) {
    return /\.(png|jpe?g|gif)$/i.test(String(name || ''));
}

// previewURL points at the fork's thumbnail endpoint. versionTime is the
// versionTime from the archive listing; omit it for the live file.
//
// Deliberately a plain URL in an <img> rather than an XHR: the session cookie
// goes with it, so it needs no key handling, and the browser gets to cache the
// archived ones -- which never change, and which the server marks accordingly.
//
// NOT under rest/. Everything there is behind the CSRF middleware and an <img>
// cannot send a header, so this endpoint is mounted beside upstream's /qr/,
// which is an <img> source for the same reason. Getting this wrong fails only
// in a browser -- the render tests stub $http and never see the middleware.
function previewURL(folder, name, versionTime) {
    var u = 'preview/?folder=' + encodeURIComponent(folder) +
        '&file=' + encodeURIComponent(name);
    if (versionTime) {
        u += '&version=' + encodeURIComponent(versionTime);
    }
    return u;
}

function matches(haystack, needle) {
    if (!needle) {
        return true;
    }
    return String(haystack).toLowerCase().indexOf(String(needle).toLowerCase()) !== -1;
}

function dayKey(d) {
    return d.getFullYear() + '-' + (d.getMonth() + 1) + '-' + d.getDate();
}

// dayTitle names the day the way somebody would out loud. Anything older than
// a week gets its date, because "last Tuesday" stops being useful about eight
// days in and starts being ambiguous.
function dayTitle(d) {
    var now = new Date();
    if (dayKey(d) === dayKey(now)) {
        return 'Today';
    }
    var yesterday = new Date(now.getTime() - 86400000);
    if (dayKey(d) === dayKey(yesterday)) {
        return 'Yesterday';
    }
    var age = (now - d) / 86400000;
    if (age < 7) {
        return ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'][d.getDay()];
    }
    return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
}

if (typeof module !== 'undefined' && module.exports) {
    module.exports = {
        pretty: pretty, matches: matches, dayKey: dayKey, dayTitle: dayTitle,
        desuqBytes: desuqBytes, previewable: previewable, previewURL: previewURL,
        conflictOriginal: conflictOriginal, prettyArchive: prettyArchive
    };
}
