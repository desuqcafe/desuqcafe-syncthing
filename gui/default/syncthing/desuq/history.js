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
                        return;
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

        // resetIfRestarted detects the daemon having restarted underneath us.
        //
        // The buffer is memory only and its ids begin again at 1, so a poll
        // with since=4211 against a fresh daemon returns events numbered from
        // 1 -- which are newer in time and lower in id. Without this the feed
        // appends them out of order and then never advances, because lastID
        // stays at 4211 forever.
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
            return row.folder + ' ' + row.name;
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

            loadDevices().finally(function () {
                refresh();
                start();
            });
        }

        function close() {
            st.open = false;
            stop();
        }

        function refresh() {
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
            if (tab === 'changes') {
                start();
                if (!st.changesReady) {
                    bootstrapChanges();
                }
            } else {
                stop();
                loadArchive();
            }
        }

        function setFolder(id) {
            st.folder = id;
            st.expanded = {};
            refresh();
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
            // Exported for custom/scripts/test-history-render.js, which
            // asserts them directly rather than through the DOM.
            _collapse: collapse,
            _intoDays: intoDays,
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
    module.exports = { pretty: pretty, matches: matches, dayKey: dayKey, dayTitle: dayTitle, desuqBytes: desuqBytes };
}
