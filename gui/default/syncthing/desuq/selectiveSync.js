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
        // Terminates the managed block: anything at the folder root that no
        // "!" line above it re-included, including things that do not exist
        // yet. Anchored, so it is the root's business only.
        var ROOT_CATCH_ALL = '/*';

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
            // Files loose at the folder root. Only ever non-zero in
            // directories-only mode, where the tree cannot show them and the
            // picker cannot offer them, so they belong to every selection.
            rootBytes: 0,
            rootFiles: 0,
            // Ignore lines outside our managed block, preserved verbatim.
            baseLines: [],
            // Exclusions read back from the managed block, waiting for the
            // tree to render so they can be unticked on it.
            pendingExclusions: [],
            // Managed lines that matched nothing in the tree; preserved too.
            staleLines: [],
            // Managed lines that match nothing in the tree because the tree is
            // showing directories only -- live rules for files it cannot draw.
            // Written back into the block unchanged. See unlistedInDirsOnly.
            unlistedLines: [],
            // Names the offering device sent that cannot safely become an
            // ignore line. Shown rather than swallowed: a file quietly missing
            // from the picker is worse than one the picker explains.
            unsafeNames: [],
            // True once anything has been unticked, so the summary can say
            // "everything" rather than a size that happens to equal the total.
            everything: true
        };

        var tree = null;
        var waitStarted = 0;
        var waitTimer = null;

        // ------------------------------------------------------------ ignores

        // Split an ignore file into "ours" and "everything else".
        //
        // The block markers are the only thing standing between the picker and
        // somebody's hand-written ignores, and the file is a plain text file
        // that anything may have edited -- the Advanced editor, a merge, a
        // half-finished paste. So the shape is checked rather than assumed:
        //
        //   - a BEGIN with no END means the rest of the file was swallowed as
        //     if the picker had written it, and the next apply would rewrite
        //     the lot. That is somebody's Blender ignore set and their
        //     "#include team.stignore" deleted without a word.
        //   - a second BEGIN before the first END is the same failure wearing
        //     a different hat.
        //
        // Neither can be repaired from here, and guessing would be worse than
        // either alternative, so the answer to both is: claim nothing. Every
        // line goes back as base, the block is reported as unusable, and the
        // caller refuses to write rather than rewriting a file it cannot
        // account for.
        function splitIgnores(lines) {
            var base = [], managed = [], inBlock = false, seenBlock = false;
            var broken = false;
            (lines || []).forEach(function (line) {
                var t = (line || '').trim();
                if (t === BEGIN) {
                    if (inBlock) { broken = true; }
                    inBlock = true;
                    seenBlock = true;
                    return;
                }
                if (t === END) {
                    if (!inBlock) { broken = true; }
                    inBlock = false;
                    return;
                }
                if (inBlock) {
                    if (t === '' || t === COMMENT_STALE) { return; }
                    managed.push(t);
                } else {
                    base.push(line);
                }
            });
            // Ran off the end still inside the block.
            if (inBlock) { broken = true; }

            if (broken) {
                return {
                    base: (lines || []).slice(),
                    managed: [],
                    seenBlock: seenBlock,
                    broken: true
                };
            }
            return { base: base, managed: managed, seenBlock: seenBlock, broken: false };
        }

        function joinIgnores(base, managed, stale) {
            var lines = (base || []).slice();
            var body = (managed || []).slice();
            if (stale && stale.length) {
                // The catch-all has to stay last or it swallows the stale
                // lines: anything at the root matches it first, so a stale
                // "!/OldProject" written after it would never re-include and a
                // stale exclusion would be redundant. Slot them in above it.
                var tail = [];
                if (body[body.length - 1] === ROOT_CATCH_ALL) {
                    tail = body.splice(body.length - 1, 1);
                }
                body.push(COMMENT_STALE);
                body = body.concat(stale, tail);
            }
            if (!body.length) {
                return lines;
            }
            lines.push(BEGIN);
            lines = lines.concat(body);
            lines.push(END);
            return lines;
        }

        // What to say when the managed block cannot be trusted. Deliberately
        // names the markers and the fix, because the person reading it is the
        // one who edited the file and is the only one who can tell which lines
        // were theirs.
        function brokenBlockMessage() {
            return 'This folder\'s ignore patterns contain a "' + BEGIN +
                '" line that does not pair with a "' + END + '" line. The picker ' +
                'cannot tell which lines it owns, and will not rewrite the file ' +
                'while that is true -- it could delete patterns you wrote. Edit ' +
                'this folder, open its Ignore Patterns tab, and either repair the ' +
                'two marker lines or delete them along with everything between ' +
                'them, then come back here.';
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
        // Two variables on purpose.
        //
        // platformEscape is the server's answer -- "|" on Windows, a backslash
        // everywhere else -- and is a property of the machine, so asking once a
        // session is right.
        //
        // escapeChar is what THIS folder uses, and is re-derived every time the
        // picker opens. They were one variable, and a folder whose own ignore
        // lines declared "#escape=" left that character cached for every folder
        // opened afterwards: the next folder, which had declared nothing,
        // quietly wrote its patterns escaped for a rule it did not have. Such
        // lines match nothing at all, and nothing anywhere reports an error --
        // the folder simply syncs the files the user unticked.
        var platformEscape = null;
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

        // Resolved per folder, from that folder's own lines. A user-supplied
        // "#escape=" wins, because by then it is the file's escape character
        // and ours would be read through it.
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
            if (platformEscape) {
                escapeChar = platformEscape;
                return $q.when(escapeChar);
            }
            return $http.get(urlbase + '/system/version').then(function (r) {
                platformEscape = (r.data && r.data.os === 'windows') ? '|' : '\\';
                escapeChar = platformEscape;
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
        // A file name is chosen by whoever offered the folder, and every name
        // here ends up as a line in .stignore. A name carrying a newline would
        // therefore write a line of its own -- "!Textures" to re-include what
        // somebody deliberately unticked, or "#include missing" to stop the
        // folder parsing its ignores and so stop it syncing at all. The server
        // rejects these too now; this is the half that can say why.
        function unsafeName(name) {
            for (var i = 0; i < name.length; i++) {
                var c = name.charCodeAt(i);
                if (c < 32 || c === 127) { return true; }
            }
            return false;
        }

        // The same name with the control characters made visible, so the
        // warning can name the file without carrying the payload into the DOM.
        function showName(name) {
            var out = '';
            for (var i = 0; i < name.length; i++) {
                var c = name.charCodeAt(i);
                out += (c < 32 || c === 127) ? '?' : name.charAt(i);
            }
            return out;
        }

        function buildSource(entries, parentPath) {
            var nodes = [];
            var bytes = 0, files = 0;
            (entries || []).forEach(function (entry) {
                if (unsafeName(entry.name || '')) {
                    st.unsafeNames.push((parentPath ? parentPath + '/' : '') +
                        showName(String(entry.name)));
                    return;
                }
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
                    node.data.dir = true;
                    if (st.dirsOnly) {
                        // /db/dirsizes gives each directory the totals for
                        // everything at or below it, children included. A node
                        // that counted that would count its subdirectories a
                        // second time through them, so what it owns is the
                        // remainder: the loose files sitting directly in it,
                        // which are the only things here no deeper node
                        // accounts for.
                        //
                        // Those files are also what a partially ticked
                        // directory keeps -- managedFor writes exclusions for
                        // the unticked children and nothing else -- so this is
                        // the same figure recount needs.
                        node.data.bytes = Math.max(0, (entry.size || 0) - sub.bytes);
                        node.data.files = Math.max(0, (entry.files || 0) - sub.files);
                        node.data.countable = true;
                        bytes += (entry.size || 0);
                        files += (entry.files || 0);
                    } else {
                        // The full tree has no aggregates: a directory weighs
                        // exactly what its children weigh, so counting it as
                        // well would double everything.
                        node.data.bytes = sub.bytes;
                        node.data.files = sub.files;
                        node.data.countable = false;
                        bytes += sub.bytes;
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

        // Walk the ticked state and emit the minimal set of exclusions below a
        // directory that was itself kept: a wholly unticked node is one line,
        // and its children need none. Only partially ticked directories are
        // descended into.
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

        // The top-level entries that survive the pick, whole or in part.
        function collectKept(root) {
            return (root.children || []).filter(function (child) {
                return child.selected || child.partsel;
            });
        }

        // Emit the managed block.
        //
        // This used to be a plain deny-list of the unticked paths, which is
        // right about everything the tree was showing and silently wrong about
        // everything it was not. A path the remote creates *after* the pick
        // matches no exclusion, so it arrives unasked -- including a whole new
        // top-level directory that was never offered and never ticked. Someone
        // who took 12 GiB of a 400 GiB share got the next project folder in
        // full, and their .stignore still looked exactly like the choice they
        // made.
        //
        // So the root is an allow-list and everything below it stays a
        // deny-list:
        //
        //     /Textures/Source     <- unticked, inside a directory we kept
        //     !/keep
        //     !/Textures           <- kept, whole or in part
        //     !/texture1.png
        //     /*                   <- anything else at the root, now or later
        //
        // Order is what makes it work: lib/ignore is first-match-wins, so the
        // exclusions have to precede the re-includes, and the catch-all has to
        // come last. A "!" on a directory covers everything under it, which is
        // what keeps the deliberate half of the old behaviour -- a file added
        // remotely inside a directory you kept still arrives without you having
        // to come back and re-tick it.
        //
        // Verified against two live instances: with this block in place a new
        // top-level directory and a new top-level file are both held back,
        // a new file inside a kept directory arrives, and a partially ticked
        // directory keeps exactly the half it was given.
        function managedFor(root) {
            // Directories-only mode lists no loose files at the root, so the
            // root is not a set this can close over: a catch-all here would
            // hold back every file sitting beside the directories purely
            // because the tree never showed it. Those files are exactly what
            // the mode's own note promises are "kept either way". So a folder
            // too big to list file by file keeps the old deny-list, and with
            // it the old gap -- named in the picker rather than left to be
            // discovered.
            if (st.dirsOnly) {
                var deny = [];
                collectExclusions(root, deny);
                // Exclusions for files this mode never showed go back in front
                // of the ones it did. Dropping them because they matched no
                // node is how a rule that was holding back 200 GB quietly
                // stops -- and unlike a stale rule, this one is still doing
                // its job. Order between the two groups does not matter here:
                // a deny-list has no re-includes for a first match to beat.
                return (st.unlistedLines || []).concat(deny.map(patternFor));
            }

            var kept = collectKept(root);
            var exclusions = [];
            kept.forEach(function (node) {
                if (node.partsel) {
                    collectExclusions(node, exclusions);
                }
            });
            // Nothing was held back at all. Writing an allow-list here would
            // hold back the *next* thing the remote adds, which is not what
            // ticking everything means -- that is what Sync Everything is for.
            if (!exclusions.length && kept.length === (root.children || []).length) {
                return [];
            }
            var lines = exclusions.map(patternFor);
            kept.forEach(function (node) {
                lines.push('!' + patternFor(node.data.path));
            });
            lines.push(ROOT_CATCH_ALL);
            return lines;
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
            // Files loose at the folder root are not in the tree in
            // directories-only mode and cannot be unticked, so they are part
            // of every selection including the empty one. Zero in the full
            // tree, where those files are ordinary nodes.
            var bytes = st.rootBytes, files = st.rootFiles, all = true;
            walk(tree.getRootNode(), function (node) {
                if (!node.data.countable) {
                    return;
                }
                // A partially ticked directory in directories-only mode keeps
                // its own loose files: managedFor excludes the children that
                // were unticked and says nothing about anything else. Counting
                // only fully ticked nodes would understate the selection by
                // exactly those files, which is the wrong direction for a
                // figure the disk guard reads.
                if (node.selected || (node.folder && node.partsel)) {
                    bytes += node.data.bytes || 0;
                    files += node.data.files || 0;
                }
                if (!node.selected) {
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
                if (st.dirsOnly) {
                    // The fork's own endpoint rather than db/browse?dirsonly=1.
                    // Both return the same directory tree; that one reaches it
                    // by skipping every file, so every directory in it reports
                    // a size of zero and the disk guard below has nothing to
                    // work with. See lib/api/api_dirsizes.go.
                    return $http.get(urlbase + '/db/dirsizes',
                        { params: { folder: folderID } });
                }
                return $http.get(urlbase + '/db/browse',
                    { params: { folder: folderID, levels: -1 } });
            }).then(function (r) {
                // buildSource appends to this as it walks, so a reload has to
                // start from empty or the warning doubles up.
                st.unsafeNames = [];
                var entries = r.data, built;
                if (st.dirsOnly) {
                    st.rootBytes = (r.data && r.data.rootBytes) || 0;
                    st.rootFiles = (r.data && r.data.rootFiles) || 0;
                    entries = (r.data && r.data.children) || [];
                    built = buildSource(entries, '');
                    // Taken from the response rather than the tree: the loose
                    // root files are real and are in neither.
                    st.totalBytes = (r.data && r.data.bytes) || 0;
                    st.totalFiles = (r.data && r.data.files) || 0;
                } else {
                    built = buildSource(entries, '');
                    st.totalBytes = built.bytes;
                    st.totalFiles = built.files;
                }
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
                            'Nothing has been downloaded. Leave it held back and this folder ' +
                            'waits, paused, until you come back to it.';
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
            st.rootBytes = 0;
            st.rootFiles = 0;
            st.globalFiles = 0;
            st.globalBytes = 0;
            st.baseLines = [];
            st.staleLines = [];
            st.unlistedLines = [];
            st.unsafeNames = [];
            st.minDiskFree = null;
            st.folderPaused = false;
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
                // The gauge beside the tree has to measure against usable
                // space, not raw free space, or it will happily tell someone a
                // 480 GB selection fits on a drive with 490 GB free and a
                // 20 GB reserve.
                st.minDiskFree = folder.minDiskFree;
                st.folderPaused = !!folder.paused;
                st.phase = 'loading';
                $http.get(urlbase + '/db/ignores?folder=' + encodeURIComponent(folder.id))
                    .then(function (r) {
                        var split = splitIgnores((r.data && r.data.ignore) || []);
                        if (split.broken) {
                            // Stop here rather than opening a picker whose
                            // Apply would rewrite lines it cannot account for.
                            // Nothing is changed, and the message names the one
                            // thing that will fix it.
                            return $q.reject({ broken: true });
                        }
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
                        if (!st.folderPaused) {
                            $rootScope.$broadcast('desuq-selective-load');
                            return null;
                        }
                        // A paused folder is not in the model at all, so
                        // /db/browse answers "no such folder" and the picker
                        // has nothing to show. Starting it is safe here for
                        // the same reason it is safe at accept time: the
                        // hold-back is still written, so the index comes back
                        // and no content does. If the picker is then closed
                        // without a choice, dismissed() pauses it again.
                        st.phase = 'starting';
                        return $http.patch(
                            urlbase + '/config/folders/' + encodeURIComponent(st.folderID),
                            { paused: false }
                        ).then(function () {
                            st.phase = 'waiting';
                            waitForIndex(st.folderID);
                        });
                    }, function (e) {
                        st.phase = 'error';
                        st.error = (e && e.broken)
                            ? brokenBlockMessage()
                            : failed('Could not read the folder', e);
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
                st.minDiskFree = folderCfg.minDiskFree;
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
                var managed = managedFor(tree.getRootNode());
                st.phase = 'applying';
                return writeIgnores(st.folderID, st.baseLines, managed, st.staleLines)
                    .then(function () {
                        // Reopened on a folder that was left held back, which
                        // means paused. The selection is written; now let it
                        // run, or the pick would appear to do nothing at all.
                        if (!st.folderPaused) {
                            return null;
                        }
                        st.folderPaused = false;
                        return $http.patch(
                            urlbase + '/config/folders/' + encodeURIComponent(st.folderID),
                            { paused: false });
                    })
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
                        // Same as apply(): a folder reopened after being left
                        // held back is paused, and taking all of it has to
                        // actually start it.
                        if (!st.folderPaused) {
                            return null;
                        }
                        st.folderPaused = false;
                        return $http.patch(
                            urlbase + '/config/folders/' + encodeURIComponent(st.folderID),
                            { paused: false });
                    })
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

            // Leave a freshly accepted share held back: the "*" stays, and the
            // folder goes back to paused so it stops advertising itself as a
            // folder that is simply up to date. "Choose Files" on the folder
            // panel reopens the picker, which is why that button no longer
            // refuses to run on a paused folder.
            holdBack: function () {
                cancelWait();
                st.open = false;
                // Escape still reaches the modal mid-save; holding back on top
                // of a selection that is already in flight would race it.
                if (!st.fresh || !st.folderID || st.phase === 'applying') {
                    return $q.when();
                }
                var folderID = st.folderID;
                st.fresh = false;
                return writeIgnores(folderID, st.baseLines, [STAR], [])
                    .then(function () {
                        return $http.patch(
                            urlbase + '/config/folders/' + encodeURIComponent(folderID),
                            { paused: true });
                    });
            },

            // Called when the modal is dismissed by any route -- the X, Escape,
            // the Close button.
            //
            // This used to clear the managed block, which deleted the "*" the
            // accept had just written on a folder that was already unpaused --
            // so closing the picker downloaded the entire share. Measured on a
            // live pair: seven files held back became twenty-six, the whole
            // folder. The screen beside that button said "Nothing is being
            // downloaded while you wait."
            //
            // "Backing out means sync everything" was a deliberate choice
            // (DEPLOYMENT-3D-TEAM.md section 2) against the alternative of a
            // folder that reports itself up to date while syncing nothing. But
            // a folder left *paused* is not that folder: it says plainly that
            // it is not running, and it downloads nothing while it waits.
            dismissed: function () {
                if (st.fresh) {
                    svc.holdBack();
                    return;
                }
                // Reopened on a folder that was paused, and closed again
                // without choosing: put it back the way it was found. open()
                // started it only so the tree could be read.
                if (!st.folderPaused || !st.folderID || st.phase === 'applying') {
                    st.open = false;
                    cancelWait();
                    return;
                }
                var folderID = st.folderID;
                st.folderPaused = false;
                cancelWait();
                st.open = false;
                $http.patch(
                    urlbase + '/config/folders/' + encodeURIComponent(folderID),
                    { paused: true });
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

                        // Turn the saved block back into ticks. A rule for
                        // something the folder no longer has is kept aside as
                        // stale rather than dropped: dropping it silently is
                        // how a rule that was holding back 200 GB quietly
                        // stops.
                        var stale = [];
                        // Live rules this mode cannot draw. See
                        // unlistedInDirsOnly below; they are written back into
                        // the managed block untouched.
                        var unlisted = [];
                        var file = function (line) {
                            if (scope.st.dirsOnly && unlistedInDirsOnly(tree, line)) {
                                unlisted.push(line);
                            } else {
                                stale.push(line);
                            }
                        };
                        var managed = scope.st.pendingExclusions || [];
                        // The catch-all is what distinguishes a block this
                        // version wrote from a plain deny-list written before
                        // the root became an allow-list. Both are read.
                        var allowList = managed.indexOf('/*') !== -1;

                        if (allowList) {
                            // Untick the root's children -- selectMode 3
                            // carries that down -- then tick back only what
                            // the "!" lines name, then apply the exclusions
                            // that sit inside them.
                            (tree.getRootNode().children || []).forEach(function (n) {
                                n.setSelected(false);
                            });
                            managed.forEach(function (line) {
                                if (line.charAt(0) !== '!') {
                                    return;
                                }
                                var node = nodeForPattern(tree, line.substring(1));
                                if (node) {
                                    node.setSelected(true);
                                } else {
                                    file(line);
                                }
                            });
                            managed.forEach(function (line) {
                                if (line.charAt(0) === '!' || line === '/*') {
                                    return;
                                }
                                var node = nodeForPattern(tree, line);
                                if (node) {
                                    node.setSelected(false);
                                } else {
                                    file(line);
                                }
                            });
                        } else {
                            // Everything starts ticked; then last time's
                            // exclusions are unticked, which propagates to
                            // ancestors on its own.
                            managed.forEach(function (line) {
                                var node = nodeForPattern(tree, line);
                                if (node) {
                                    node.setSelected(false);
                                } else if (line !== '*') {
                                    file(line);
                                }
                            });
                        }
                        scope.st.staleLines = stale;
                        scope.st.unlistedLines = unlisted;
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

                // In directories-only mode "no node for this pattern" does not
                // mean the rule is dead. The tree holds no files at all, so
                // every file-level exclusion from an earlier pick -- made when
                // the folder was still small enough to list -- matches nothing
                // here and would be filed as stale. It is not: the file exists
                // and the rule is holding it back, which was the entire point.
                //
                // The test is whether the directory the pattern sits in is on
                // the tree. If it is, the pattern names something inside a
                // directory that really is there, so it is a live rule this
                // mode simply cannot draw. If it is not, the directory itself
                // is gone and the rule is stale for the ordinary reason.
                //
                // A pattern with no directory part is at the folder root,
                // which this mode cannot list either -- so it gets the same
                // benefit of the doubt.
                function unlistedInDirsOnly(tree, line) {
                    var body = line.charAt(0) === '!' ? line.substring(1) : line;
                    if (body.charAt(0) !== '/') {
                        return false;
                    }
                    var path = desuqSelective.unescapeGlob(body.substring(1));
                    var cut = path.lastIndexOf('/');
                    if (cut < 0) {
                        return true;
                    }
                    return !!tree.getNodeByKey(path.substring(0, cut));
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
