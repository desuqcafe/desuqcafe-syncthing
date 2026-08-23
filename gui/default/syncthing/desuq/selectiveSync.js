// Added by the desuqcafe fork. Lives in its own directory, which upstream does
// not have, so it can never conflict on a merge.
//
// Selective receive: a "tick what you want" picker.
//
// Syncthing has no per-file subscription UI. The only mechanism is .stignore,
// and the accept-time flow drops you into a textarea of glob patterns -- which
// for the people this fork is built for is the same as having no mechanism at
// all. See custom/DEPLOYMENT-3D-TEAM.md section 2.
//
// The trick that makes a real picker possible is that /rest/db/browse serves
// the *global* tree: it calls GlobalDirectoryTree, which walks the global
// index rather than the local disk, so it lists every file the offering device
// has, including ones that were never pulled. So the sequence is:
//
//   1. create the folder paused,
//   2. write "*" as the only ignore line,
//   3. start it -- the index arrives, no file data does,
//   4. browse the tree, let the user tick,
//   5. write the *unticked* paths as ignores, and the rest pulls.
//
// Step 2 is what makes this safe: between accepting the share and choosing,
// not one byte of content is downloaded. Verified against two live instances:
// with "*" set, globalFiles was 17 and localFiles 0, and the full tree was
// still browsable.
//
// Exclusions rather than inclusions is deliberate. It means a file added
// remotely inside a directory you kept arrives without you having to come back
// and re-tick it, which is what someone who ticked the directory meant.
//
// Nothing here needs a new REST route or any server code.

angular.module('syncthing.core')

    .factory('desuqSelective', function ($http, $q, $timeout, $rootScope) {
        'use strict';

        // The picker owns the lines between these two markers and rewrites
        // them wholesale. Everything outside is preserved untouched, which is
        // what keeps the seeded Blender ignore block -- and the
        // "#include team.stignore" line from DEPLOYMENT-3D-TEAM.md section 2 --
        // alive across a re-pick.
        var BEGIN = '//// desuqcafe selective sync -- rewritten by the file picker, do not hand edit';
        var END = '//// desuqcafe selective sync -- end';
        var COMMENT_STALE = '//// kept from a previous pick; matches nothing in the folder as it stands now';

        // Everything ignored. Written at accept time so the index can arrive
        // while no content does.
        var STAR = '*';

        // Past this many files the whole-tree fetch gets slow and fancytree
        // gets unusable, so the picker drops to directories only. A texture
        // library can easily be six figures.
        var FULL_TREE_LIMIT = 20000;

        // How long to wait for the offering device to send an index before
        // giving up and offering a way out. A disconnected peer never sends
        // one at all.
        var INDEX_WAIT_MS = 90000;
        var INDEX_POLL_MS = 1000;

        var st = {
            open: false,
            // creating | starting | waiting | loading | ready | applying | error
            phase: 'ready',
            folderID: null,
            folderLabel: '',
            folderPath: '',
            // Set when the picker is driving a freshly accepted share, in
            // which case backing out has to mean something safe.
            fresh: false,
            error: null,
            dirsOnly: false,
            globalFiles: 0,
            globalBytes: 0,
            selectedBytes: 0,
            selectedFiles: 0,
            totalBytes: 0,
            totalFiles: 0,
            // Ignore lines outside our managed block, preserved verbatim.
            baseLines: [],
            // Exclusions read back from the managed block, waiting for the
            // tree to render so they can be unticked on it.
            pendingExclusions: [],
            // Managed lines that matched nothing in the tree; preserved too.
            staleLines: [],
            // True once anything has been unticked, so the summary can say
            // "everything" rather than a size that happens to equal the total.
            everything: true
        };

        var tree = null;
        var waitStarted = 0;
        var waitTimer = null;

        // ------------------------------------------------------------ ignores

        function splitIgnores(lines) {
            var base = [], managed = [], inBlock = false, seenBlock = false;
            (lines || []).forEach(function (line) {
                var t = (line || '').trim();
                if (t === BEGIN) { inBlock = true; seenBlock = true; return; }
                if (t === END) { inBlock = false; return; }
                if (inBlock) {
                    if (t === '' || t === COMMENT_STALE) { return; }
                    managed.push(t);
                } else {
                    base.push(line);
                }
            });
            return { base: base, managed: managed, seenBlock: seenBlock };
        }

        function joinIgnores(base, managed, stale) {
            var lines = (base || []).slice();
            var body = (managed || []).slice();
            if (stale && stale.length) {
                body.push(COMMENT_STALE);
                body = body.concat(stale);
            }
            if (!body.length) {
                return lines;
            }
            lines.push(BEGIN);
            lines = lines.concat(body);
            lines.push(END);
            return lines;
        }

        // A parse failure is not a failed request: the lines were accepted and
        // written, and the folder has stopped because of them. Reporting it as
        // "could not save" would send someone looking in the wrong place.
        function failed(what, e) {
            if (e && e.parse) {
                return 'The ignore patterns were saved, but Syncthing cannot read ' +
                    'them, so this folder will not sync until they are fixed: ' +
                    String(e.data);
            }
            return what + ': ' +
                ((e && e.data) ? String(e.data) : 'the request failed') + '.';
        }

        // The POST replies with the same body as GET /db/ignores, which
        // carries a parse error when Syncthing could not make sense of what it
        // was handed. That matters more here than anywhere else in the GUI: a
        // folder whose ignore file will not parse refuses to scan or pull at
        // all, so letting the failure pass looks exactly like a folder that
        // simply never syncs. The likeliest cause is the "#include
        // team.stignore" recipe on a folder that has not pulled that file yet
        // -- see DEPLOYMENT-3D-TEAM.md section 2.
        function writeIgnores(folderID, base, managed, stale) {
            return $http.post(urlbase + '/db/ignores?folder=' + encodeURIComponent(folderID), {
                ignore: joinIgnores(base, managed, stale)
            }).then(function (r) {
                if (r.data && r.data.error) {
                    return $q.reject({ data: r.data.error, parse: true });
                }
                return r;
            });
        }

        // Ignore patterns are globs, so a literal name containing a glob
        // metacharacter has to be escaped or the pattern silently matches
        // things it was never meant to -- "asset[1].png" is a glob for
        // "asset1.png", which in a render output directory is very likely to
        // be sitting right next to it.
        //
        // Which character escapes is platform-dependent, and getting it wrong
        // fails quietly rather than loudly. lib/ignore picks the escape
        // character at init: backslash normally, but *pipe on Windows*,
        // because backslash is the path separator there. Writing a backslash
        // on Windows does not escape anything -- the parser runs ToSlash over
        // the line first, so "asset\[1\].png" becomes "asset/[1/].png" and
        // matches nothing at all.
        //
        // An explicit "#escape=" line would settle it, but the parser rejects
        // one that appears after any pattern, and ours would have to go after
        // the user's own lines. So ask the server what it is instead.
        var escapeChar = null;

        var GLOBBY = '*?[]{}';

        function escapeGlob(name) {
            var out = '';
            for (var i = 0; i < name.length; i++) {
                var c = name.charAt(i);
                if (c === escapeChar || GLOBBY.indexOf(c) >= 0) {
                    out += escapeChar;
                }
                out += c;
            }
            return out;
        }

        function unescapeGlob(pattern) {
            var out = '';
            for (var i = 0; i < pattern.length; i++) {
                if (pattern.charAt(i) === escapeChar && i + 1 < pattern.length) {
                    i++;
                }
                out += pattern.charAt(i);
            }
            return out;
        }

        function patternFor(path) {
            // Leading slash anchors the pattern at the folder root, so
            // "/Textures/Source" excludes exactly that directory and not a
            // "Source" somewhere else. Syncthing expands it to both
            // "/textures/source" and "/textures/source/**".
            return '/' + path.split('/').map(escapeGlob).join('/');
        }

        // Resolved once per session. A user-supplied "#escape=" among the
        // folder's own lines wins, because by then it is the file's escape
        // character and ours would be read through it.
        function resolveEscapeChar(baseLines) {
            var declared = null;
            (baseLines || []).forEach(function (line) {
                var m = /^#escape\s*=\s*(\S)\s*$/.exec(line || '');
                if (m) { declared = m[1]; }
            });
            if (declared) {
                escapeChar = declared;
                return $q.when(escapeChar);
            }
            if (escapeChar) {
                return $q.when(escapeChar);
            }
            return $http.get(urlbase + '/system/version').then(function (r) {
                escapeChar = (r.data && r.data.os === 'windows') ? '|' : '\\';
                return escapeChar;
            }, function () {
                // The picker cannot run without having asked, and guessing
                // wrong writes rules that match nothing.
                return $q.reject({ data: 'could not ask Syncthing which platform it is on' });
            });
        }

        // ------------------------------------------------------------- tree

        function isDir(entry) {
            return entry.type === 'FILE_INFO_TYPE_DIRECTORY';
        }

        // db/browse reports 0 (or a filesystem-dependent stub) for directory
        // sizes, so a directory's weight has to be summed from its children.
        function buildSource(entries, parentPath) {
            var nodes = [];
            var bytes = 0, files = 0;
            (entries || []).forEach(function (entry) {
                var path = parentPath ? parentPath + '/' + entry.name : entry.name;
                var node = {
                    title: entry.name,
                    key: path,
                    selected: true,
                    data: { path: path }
                };
                if (isDir(entry)) {
                    var sub = buildSource(entry.children, path);
                    node.folder = true;
                    node.children = sub.nodes;
                    node.data.bytes = sub.bytes;
                    node.data.files = sub.files;
                    node.data.dir = true;
                    // In directories-only mode a directory is the unit being
                    // picked, so it counts as one item, and the running total
                    // has to count it as well as everything below it.
                    // Otherwise its weight is entirely its children's and
                    // counting it too would double it.
                    node.data.countable = st.dirsOnly;
                    bytes += sub.bytes;
                    if (st.dirsOnly) {
                        node.data.files = 1;
                        files += 1 + sub.files;
                    } else {
                        files += sub.files;
                    }
                } else {
                    node.data.bytes = entry.size || 0;
                    node.data.files = 1;
                    node.data.countable = true;
                    bytes += node.data.bytes;
                    files += 1;
                }
                nodes.push(node);
            });
            // Directories first, then case-insensitive by name. Doing it here
            // rather than in fancytree's sortChildren keeps the render cheap.
            nodes.sort(function (a, b) {
                var x = (a.folder ? '0' : '1') + a.title.toLowerCase();
                var y = (b.folder ? '0' : '1') + b.title.toLowerCase();
                return x === y ? 0 : x > y ? 1 : -1;
            });
            return { nodes: nodes, bytes: bytes, files: files };
        }

        // Walk the ticked state and emit the minimal set of exclusions: a
        // wholly unticked node is one line, and its children need none. Only
        // partially ticked directories are descended into, which is also why
        // no "!" re-include lines are ever needed.
        function collectExclusions(node, out) {
            (node.children || []).forEach(function (child) {
                if (child.selected) {
                    return;
                }
                if (child.partsel) {
                    collectExclusions(child, out);
                    return;
                }
                out.push(child.data.path);
            });
        }

        function walk(node, fn) {
            (node.children || []).forEach(function (child) {
                fn(child);
                walk(child, fn);
            });
        }

        function recount() {
            if (!tree) {
                return;
            }
            var bytes = 0, files = 0, all = true;
            walk(tree.getRootNode(), function (node) {
                if (!node.data.countable) {
                    return;
                }
                if (node.selected) {
                    bytes += node.data.bytes || 0;
                    files += node.data.files || 0;
                } else {
                    all = false;
                }
            });
            st.selectedBytes = bytes;
            st.selectedFiles = files;
            st.everything = all;
        }

        // Ticking a directory fires one select event per node underneath it,
        // and recount walks the whole tree. On a twenty-thousand-node folder
        // doing that per event is the difference between instant and a
        // visible stall.
        var recountTimer = null;
        function recountSoon() {
            if (recountTimer) {
                $timeout.cancel(recountTimer);
            }
            recountTimer = $timeout(function () {
                recountTimer = null;
                recount();
            }, 30);
        }

        // ------------------------------------------------------------- load

        function loadStatus(folderID) {
            return $http.get(urlbase + '/db/status?folder=' + encodeURIComponent(folderID))
                .then(function (r) { return r.data; });
        }

        function loadTree(folderID) {
            st.phase = 'loading';
            return loadStatus(folderID).then(function (status) {
                st.globalFiles = status.globalFiles || 0;
                st.globalBytes = status.globalBytes || 0;
                // Deciding from the count rather than from the response keeps
                // the browser out of a multi-megabyte JSON parse it cannot
                // render anyway.
                st.dirsOnly = st.globalFiles > FULL_TREE_LIMIT;
                var params = { folder: folderID, levels: -1 };
                if (st.dirsOnly) {
                    params.dirsonly = 1;
                }
                return $http.get(urlbase + '/db/browse', { params: params });
            }).then(function (r) {
                var built = buildSource(r.data, '');
                st.totalBytes = built.bytes;
                st.totalFiles = built.files;
                return built.nodes;
            });
        }

        function waitForIndex(folderID) {
            waitStarted = Date.now();
            var poll = function () {
                waitTimer = null;
                if (!st.open || st.folderID !== folderID) {
                    return;
                }
                loadStatus(folderID).then(function (status) {
                    if ((status.globalFiles || 0) + (status.globalDirectories || 0) > 0) {
                        st.phase = 'loading';
                        $rootScope.$broadcast('desuq-selective-load');
                        return;
                    }
                    if (Date.now() - waitStarted > INDEX_WAIT_MS) {
                        st.phase = 'error';
                        st.error = 'No file list has arrived from the other device yet. ' +
                            'That usually means it is offline, or has not finished scanning. ' +
                            'Nothing has been downloaded; you can close this and pick later.';
                        return;
                    }
                    waitTimer = $timeout(poll, INDEX_POLL_MS);
                }, function () {
                    waitTimer = $timeout(poll, INDEX_POLL_MS);
                });
            };
            waitTimer = $timeout(poll, INDEX_POLL_MS);
        }

        function cancelWait() {
            if (waitTimer) {
                $timeout.cancel(waitTimer);
                waitTimer = null;
            }
        }

        // ------------------------------------------------------------- public

        function reset() {
            cancelWait();
            if (recountTimer) {
                $timeout.cancel(recountTimer);
                recountTimer = null;
            }
            tree = null;
            st.phase = 'loading';
            st.error = null;
            st.pendingExclusions = [];
            st.dirsOnly = false;
            st.selectedBytes = 0;
            st.selectedFiles = 0;
            st.totalBytes = 0;
            st.totalFiles = 0;
            st.globalFiles = 0;
            st.globalBytes = 0;
            st.baseLines = [];
            st.staleLines = [];
            st.everything = true;
        }

        var svc = {
            state: st,

            // Exposed so the directive can turn a stored pattern back into
            // the tree key it came from, using whichever escape character
            // this server actually uses.
            unescapeGlob: unescapeGlob,

            setTree: function (t) {
                tree = t;
                recount();
            },

            onSelect: function () {
                recountSoon();
            },

            // Tick or untick everything at once. The two most common answers
            // are "all of it" and "just this one directory", and the second is
            // far quicker to reach from a cleared tree.
            selectAll: function (on) {
                if (!tree) {
                    return;
                }
                tree.getRootNode().visit(function (node) {
                    node.setSelected(on);
                });
                recount();
            },

            loadTree: loadTree,

            // Existing folder: read back what was picked last time and show it.
            open: function (folder) {
                reset();
                st.open = true;
                st.fresh = false;
                st.folderID = folder.id;
                st.folderLabel = folder.label || folder.id;
                st.folderPath = folder.path;
                st.phase = 'loading';
                $http.get(urlbase + '/db/ignores?folder=' + encodeURIComponent(folder.id))
                    .then(function (r) {
                        var split = splitIgnores((r.data && r.data.ignore) || []);
                        st.baseLines = split.base;
                        st.pendingExclusions = split.managed;
                    }, function () {
                        // A folder with no ignore file at all reads as an
                        // error; that just means nothing has been excluded.
                        st.baseLines = [];
                        st.pendingExclusions = [];
                    })
                    .then(function () {
                        return resolveEscapeChar(st.baseLines);
                    })
                    .then(function () {
                        $rootScope.$broadcast('desuq-selective-load');
                    }, function (e) {
                        st.phase = 'error';
                        st.error = failed('Could not read the folder', e);
                    });
            },

            // Freshly accepted share. Called from saveFolder in place of the
            // rest of the save, so this owns creating the folder too.
            acceptAndPick: function (folderCfg) {
                reset();
                st.open = true;
                st.fresh = true;
                st.folderID = folderCfg.id;
                st.folderLabel = folderCfg.label || folderCfg.id;
                st.folderPath = folderCfg.path;
                st.phase = 'creating';
                st.pendingExclusions = [];

                var cfg = {};
                angular.forEach(folderCfg, function (v, k) {
                    // The editor's own scratch fields; harmless, but no reason
                    // to post them.
                    if (k.charAt(0) !== '_') { cfg[k] = v; }
                });
                // Paused, so that nothing at all happens between the folder
                // existing and "*" being written to its ignores.
                cfg.paused = true;

                $http.post(urlbase + '/config/folders', cfg)
                    .then(function () {
                        return $http.get(urlbase + '/config/defaults/ignores');
                    })
                    .then(function (r) {
                        st.baseLines = (r.data && r.data.lines) || [];
                    }, function () {
                        // No defaults configured is not an error.
                        st.baseLines = [];
                    })
                    .then(function () {
                        return resolveEscapeChar(st.baseLines);
                    })
                    .then(function () {
                        return writeIgnores(st.folderID, st.baseLines, [STAR], []);
                    })
                    .then(function () {
                        st.phase = 'starting';
                        return $http.patch(
                            urlbase + '/config/folders/' + encodeURIComponent(st.folderID),
                            { paused: false });
                    })
                    .then(function () {
                        st.phase = 'waiting';
                        waitForIndex(st.folderID);
                    })
                    .catch(function (e) {
                        st.phase = 'error';
                        st.error = failed('Could not set the folder up', e);
                    });
            },

            // Write the ticked selection and let it pull.
            apply: function () {
                if (!tree) {
                    return $q.reject();
                }
                var exclusions = [];
                collectExclusions(tree.getRootNode(), exclusions);
                var managed = exclusions.map(patternFor);
                st.phase = 'applying';
                return writeIgnores(st.folderID, st.baseLines, managed, st.staleLines)
                    .then(function () {
                        // A choice has been recorded, so backing out of the
                        // modal must no longer fall back to "sync everything".
                        st.fresh = false;
                        svc.close();
                    }, function (e) {
                        st.phase = 'error';
                        st.error = failed('Could not save the selection', e);
                    });
            },

            // Take the lot. Also the safe answer if the picker is backed out
            // of on a fresh accept, because leaving "*" in place would mean a
            // folder that reports itself up to date while syncing nothing.
            syncEverything: function () {
                st.phase = 'applying';
                return writeIgnores(st.folderID, st.baseLines, [], st.staleLines)
                    .then(function () {
                        st.fresh = false;
                        svc.close();
                    }, function (e) {
                        st.phase = 'error';
                        st.error = failed('Could not clear the selection', e);
                    });
            },

            close: function () {
                cancelWait();
                st.open = false;
                $rootScope.$broadcast('desuq-selective-close');
            },

            // Called when the modal is dismissed by any route -- the X, the
            // backdrop, Escape. On a fresh accept that would otherwise strand
            // the folder on "*".
            dismissed: function () {
                cancelWait();
                st.open = false;
                if (st.fresh && st.folderID && st.phase !== 'applying') {
                    st.fresh = false;
                    writeIgnores(st.folderID, st.baseLines, [], st.staleLines);
                }
            }
        };

        return svc;
    })

    // Published on $rootScope so that the single fork line in
    // syncthingController's saveFolder can reach it without an edit to that
    // controller's dependency list. It is also guarded there, so with this
    // file removed the branch is dead and the save behaves exactly as
    // upstream's.
    .run(function ($rootScope, desuqSelective) {
        $rootScope.desuqSelective = desuqSelective;
    })

    // The option offered in the folder editor's Ignores tab when adding or
    // accepting a folder. Ticking it hands the save over to the picker.
    .directive('desuqSelectiveOption', function () {
        return {
            restrict: 'E',
            scope: false,
            template:
                '<div class="desuq-pick-offer" ng-class="{\'desuq-pick-offer-on\': currentFolder._desuqPick}">' +
                '  <label class="desuq-pick-offer-head">' +
                '    <input type="checkbox" ng-model="currentFolder._desuqPick" ng-change="desuqPickChanged()"/>' +
                '    <span class="desuq-pick-offer-title">Choose what to sync</span>' +
                '    <span class="desuq-pick-offer-badge">picker</span>' +
                '  </label>' +
                '  <p class="help-block desuq-pick-offer-body">' +
                '    Adds the folder with everything held back, waits for the other device to send its' +
                '    file list, then shows you the whole tree so you can tick only the parts you want.' +
                '    <strong>Nothing is downloaded until you choose.</strong>' +
                '  </p>' +
                '</div>',
            link: function (scope) {
                scope.desuqPickChanged = function () {
                    // The two are alternative answers to the same question,
                    // and upstream's flow would otherwise open its ignore
                    // textarea on top of the picker.
                    if (scope.currentFolder._desuqPick) {
                        scope.currentFolder._addIgnores = false;
                    }
                };
                scope.$watch('currentFolder._addIgnores', function (on) {
                    if (on && scope.currentFolder) {
                        scope.currentFolder._desuqPick = false;
                    }
                });
            }
        };
    })

    // The picker itself.
    .directive('desuqSelectiveModal', function (desuqSelective, $timeout) {
        return {
            restrict: 'E',
            templateUrl: 'syncthing/desuq/selectiveSyncModalView.html',
            scope: {},
            link: function (scope) {
                var $modal = null;
                scope.st = desuqSelective.state;
                scope.svc = desuqSelective;
                scope.filter = '';

                function element() {
                    if (!$modal) {
                        $modal = $('#desuqSelective');
                        $modal.on('hidden.bs.modal', function () {
                            desuqSelective.dismissed();
                            destroyTree();
                            scope.$applyAsync();
                        });
                    }
                    return $modal;
                }

                function destroyTree() {
                    var $tree = $('#desuqSelectiveTree');
                    if ($tree.length && $tree.data('ui-fancytree')) {
                        $tree.fancytree('destroy');
                    }
                    $tree.empty();
                    desuqSelective.setTree(null);
                }

                scope.$watch('st.open', function (open, was) {
                    if (open === was) {
                        return;
                    }
                    if (open) {
                        // The picker is normally opened as the folder editor
                        // closes. Bootstrap 3 drops "modal-open" off <body>
                        // when the first of two overlapping modals finishes
                        // hiding, which leaves the second one unscrollable, so
                        // wait the hide out rather than racing it.
                        var $editor = $('#editFolder');
                        if (($editor.data('bs.modal') || {}).isShown) {
                            $editor.one('hidden.bs.modal', function () {
                                $timeout(function () {
                                    if (desuqSelective.state.open) {
                                        element().modal({ backdrop: 'static', keyboard: true });
                                    }
                                });
                            });
                        } else {
                            element().modal({ backdrop: 'static', keyboard: true });
                        }
                    } else {
                        element().modal('hide');
                    }
                });

                scope.$on('desuq-selective-load', function () {
                    // $timeout so the modal body exists before fancytree is
                    // attached to it.
                    $timeout(function () {
                        desuqSelective.loadTree(desuqSelective.state.folderID)
                            .then(render, function (e) {
                                scope.st.phase = 'error';
                                scope.st.error = 'Could not read the file list: ' +
                                    ((e && e.data) ? String(e.data) : 'the request failed') + '.';
                            });
                    });
                });

                function render(source) {
                    destroyTree();
                    scope.st.phase = 'ready';
                    // Another $timeout: phase only becomes 'ready' on this
                    // digest, and the tree container is ng-if'd on it.
                    $timeout(function () {
                        var tree = $('#desuqSelectiveTree').fancytree({
                            extensions: ['filter', 'glyph'],
                            checkbox: true,
                            // Hierarchical: ticking a directory ticks its
                            // contents, and a directory with only some of its
                            // contents ticked renders as partially selected --
                            // which is exactly the state the exclusion walk
                            // needs to descend into.
                            selectMode: 3,
                            // Node titles are remote-controlled file names.
                            escapeTitles: true,
                            debugLevel: 0,
                            glyph: { preset: 'awesome5' },
                            filter: { mode: 'hide', autoExpand: true, highlight: true },
                            source: source,
                            select: function () {
                                desuqSelective.onSelect();
                                scope.$applyAsync();
                            },
                            renderNode: function (event, data) {
                                var node = data.node;
                                if (!node.span || scope.st.dirsOnly) {
                                    return;
                                }
                                // Checked rather than latched: fancytree
                                // re-renders a node on filter and on status
                                // change, and a latch would leave the size off
                                // whenever it rebuilt the span.
                                if (node.span.querySelector('.desuq-pick-size')) {
                                    return;
                                }
                                var span = document.createElement('span');
                                span.className = 'desuq-pick-size';
                                span.textContent = humanBytes(node.data.bytes || 0);
                                node.span.appendChild(span);
                            }
                        }).fancytree('getTree');

                        // Everything starts ticked; then last time's
                        // exclusions are unticked, which propagates to
                        // ancestors on its own.
                        var stale = [];
                        (scope.st.pendingExclusions || []).forEach(function (line) {
                            var node = nodeForPattern(tree, line);
                            if (node) {
                                node.setSelected(false);
                            } else if (line !== '*') {
                                // A rule for something the folder no longer
                                // has. Dropping it silently is how a rule that
                                // was holding back 200 GB quietly stops.
                                stale.push(line);
                            }
                        });
                        scope.st.staleLines = stale;
                        // Expand the first level so the folder does not open
                        // as a single collapsed row.
                        tree.getRootNode().children.forEach(function (n) {
                            if (n.folder) { n.setExpanded(true); }
                        });
                        desuqSelective.setTree(tree);
                        scope.$applyAsync();
                    });
                }

                // Reverse of patternFor: strip the anchor and the escapes.
                function nodeForPattern(tree, line) {
                    if (line.charAt(0) !== '/') {
                        return null;
                    }
                    return tree.getNodeByKey(
                        desuqSelective.unescapeGlob(line.substring(1)));
                }

                function humanBytes(n) {
                    if (!n) { return ''; }
                    var units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
                    var i = 0;
                    while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
                    return (i === 0 ? n : n.toFixed(n < 10 ? 1 : 0)) + ' ' + units[i];
                }

                scope.applyFilter = function () {
                    var tree = $('#desuqSelectiveTree').fancytree('getTree');
                    if (!tree) {
                        return;
                    }
                    if (scope.filter) {
                        tree.filterNodes(scope.filter, { autoExpand: true });
                    } else {
                        tree.clearFilter();
                    }
                };

                scope.$on('$destroy', destroyTree);
            }
        };
    });
