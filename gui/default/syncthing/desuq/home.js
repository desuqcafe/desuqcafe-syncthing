// desuqcafe fork: the main screen.
//
// The default view answers three questions and nothing else: is everything
// here, who am I sharing with, is anything wrong. Upstream's own main screen
// answers none of them -- it reports plumbing (download rate, listeners,
// uptime, connection type, compression) at the same visual weight as the two
// facts a modeller actually needs.
//
// This is deliberately a different screen rather than upstream's with fields
// hidden, because hiding fields leaves the same shape and the shape is the
// problem.
//
// TWO RULES THE LAYOUT ENFORCES
//
// Violet is the resting colour. The good and busy states are what anybody sees
// almost all of the time, so those are the fork's own colour and amber and red
// mean something precisely because nothing else competes with them. There is
// no green anywhere.
//
// Calm is quiet. A banner announcing that nothing is wrong is still noise, and
// it trains the eye to skip the one place a real problem will appear. So the
// headline's *weight* is proportional to how much it wants from you: settled
// states are a single unboxed line, and only attention and problem get the
// full bordered treatment.
//
// WHY "IS EVERYTHING HERE" IS NOT A LOCAL QUESTION
//
// The first version of this file derived the headline from local folder state
// alone, and cheerfully reported "Everything is here, in step with Yuki" about
// a folder Yuki had never accepted -- locally idle, nothing needed, and zero
// bytes actually shared. Whether your work reached anybody lives in
// /rest/db/completion per folder per device, so that is what the strip on each
// folder card and half the headline rules below are built from.
//
// WHY IT READS REST RATHER THAN syncthingController's SCOPE
//
// The same reason the first-run wizard does: it keeps syncthingController.js
// untouched, and it lets custom/scripts/test-home-render.js drive the whole
// screen against canned REST without standing up the controller.
//
// The directive's *template* is a different matter -- it uses a child scope
// (scope: true) rather than an isolate one, so ng-click can call the
// controller's own addFolder(), editFolderExisting(), setFolderPause() and the
// rest by name. Reimplementing those would duplicate upstream logic for no
// gain; calling them costs nothing at merge time because it does not edit the
// file they live in.

angular.module('syncthing.core')

    .factory('desuqHome', function ($http, $q, $window) {
        'use strict';

        // Everything here is local and small. The screen is always on, so this
        // is the fork's only permanent poll; 2.5s is slow enough to be free and
        // fast enough that finishing a sync feels immediate.
        var POLL_MS = 2500;

        // folders x devices completion requests per tick. Two folders and two
        // peers is four; this audience will not reach the cap, but a shared
        // machine with twenty folders should not quietly start hammering.
        var MAX_COMPLETION_REQUESTS = 24;

        // Written by deviceHandshakeDirective.js, read (never written) here so
        // a peer card can say whether it has been verified on this computer.
        var HANDSHAKE_STORE = 'desuq.handshake.confirmed';

        var st = {
            ready: false,
            folders: [],
            peers: [],
            errors: [],
            // folder id -> the dry run from /rest/db/reclaimable, only for
            // folders that have something to reclaim.
            reclaimable: {},
            // {tone, text, detail} -- tone drives colour and weight, nothing else.
            headline: { tone: 'busy', text: 'Checking…', detail: '' }
        };

        function confirmations() {
            try {
                return JSON.parse($window.localStorage.getItem(HANDSHAKE_STORE)) || {};
            } catch (e) {
                return {};
            }
        }

        function normalise(id) {
            return String(id || '').toUpperCase().replace(/[^A-Z0-9]/g, '');
        }

        // Bytes, in the units a person uses out loud. Deliberately not
        // upstream's binary/decimal toggle: this string appears in a sentence,
        // and a sentence should not change units under the reader.
        function size(bytes) {
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

        function plural(n, one, many) {
            return n === 1 ? one : many;
        }

        function joinNames(list) {
            if (list.length <= 1) {
                return list[0] || '';
            }
            if (list.length === 2) {
                return list[0] + ' and ' + list[1];
            }
            return list.slice(0, -1).join(', ') + ' and ' + list[list.length - 1];
        }

        // One or two letters, for the people strip. Two initials where the name
        // has two words, because "Y" for both "Yuki" and "Yusuke" defeats the
        // point of showing who.
        function initials(name) {
            var parts = String(name || '?').trim().split(/[\s_-]+/).filter(Boolean);
            if (!parts.length) {
                return '?';
            }
            if (parts.length === 1) {
                return parts[0].substring(0, 1).toUpperCase();
            }
            return (parts[0].substring(0, 1) + parts[parts.length - 1].substring(0, 1)).toUpperCase();
        }

        // The same state machine as syncthingController's folderStatus(),
        // derived from REST instead of from $scope.model. Kept in step with it
        // deliberately -- including the fork's own rule that a receive-only
        // folder holding local additions is a warning and not a success.
        function folderState(cfg, status) {
            if (cfg.paused) {
                return 'paused';
            }
            if (!status || !status.state) {
                return 'unknown';
            }
            var state = '' + status.state;
            if (state === 'error') {
                return 'stopped';
            }
            if (state !== 'idle') {
                return state;
            }
            if ((status.needTotalItems || 0) > 0) {
                return 'outofsync';
            }
            if ((status.errors || 0) > 0) {
                return 'faileditems';
            }
            if ((status.receiveOnlyTotalItems || 0) > 0) {
                return cfg.type === 'receiveonly' ? 'localadditions' : 'localunencrypted';
            }
            if ((cfg.devices || []).length <= 1) {
                return 'unshared';
            }
            return state;
        }

        // How one other person stands on one folder. `remoteState` is the field
        // that distinguishes "behind" from "has not accepted the share at all",
        // which look identical if you only read the percentage.
        function shareOf(peer, completion) {
            var c = completion || {};
            var pct = typeof c.completion === 'number' ? c.completion : null;
            var kind;
            if (c.remoteState === 'notSharing') {
                kind = 'notaccepted';
            } else if (c.remoteState === 'paused' || peer.paused) {
                kind = 'paused';
            } else if (pct === null) {
                kind = 'unknown';
            } else if (pct >= 100) {
                kind = 'complete';
            } else {
                kind = 'behind';
            }
            return {
                deviceID: peer.deviceID,
                name: peer.name,
                initials: initials(peer.name),
                connected: peer.connected,
                kind: kind,
                pct: pct === null ? null : Math.max(0, Math.min(100, Math.round(pct))),
                needBytes: c.needBytes || 0
            };
        }

        // One sentence under the people strip. The strip says who; this says
        // what it means, because a row of coloured initials is a legend nobody
        // was given.
        function peopleNote(people, heldBack) {
            if (!people.length) {
                return 'Not shared with anybody yet.';
            }
            // "has the same files as you" is a claim about both sides, and it
            // is false the moment selective sync is in play: they have the
            // whole folder, this computer has the part somebody picked. Say
            // what is actually true instead, and in the direction that
            // matters -- their copy is the complete one.
            if (heldBack > 0) {
                var all = people.filter(function (s) { return s.kind === 'complete'; })
                    .map(function (s) { return s.name; });
                if (all.length === people.length) {
                    return joinNames(all) + ' ' + plural(all.length, 'has', 'have') +
                        ' the whole folder. You have chosen part of it.';
                }
            }
            var by = function (k) {
                return people.filter(function (s) { return s.kind === k; })
                    .map(function (s) { return s.name; });
            };
            var notAccepted = by('notaccepted');
            if (notAccepted.length) {
                return joinNames(notAccepted) + ' ' + plural(notAccepted.length, 'has', 'have') +
                    ' not accepted this folder yet.';
            }
            var behind = people.filter(function (s) { return s.kind === 'behind'; });
            if (behind.length) {
                var pending = behind.reduce(function (a, s) { return a + s.needBytes; }, 0);
                return joinNames(behind.map(function (s) { return s.name; })) + ' ' +
                    plural(behind.length, 'is', 'are') + ' still catching up' +
                    (pending > 0 ? ' — ' + size(pending) + ' to go.' : '.');
            }
            var paused = by('paused');
            if (paused.length === people.length) {
                return joinNames(paused) + ' ' + plural(paused.length, 'has', 'have') + ' this folder paused.';
            }
            var complete = by('complete');
            if (complete.length === people.length) {
                return people.length === 1
                    ? complete[0] + ' has the same files as you.'
                    : 'Everyone has the same files.';
            }
            return joinNames(people.map(function (s) { return s.name; })) + ' ' +
                plural(people.length, 'is', 'are') + ' sharing this folder.';
        }

        // One line for a person's card, saying what is actually true of them
        // rather than what the config says.
        //
        // "You share Project Assets with them" was the first version, and it
        // could sit directly under a banner reading "Yuki has not accepted
        // Project Assets yet" -- because it described the folder's device list
        // and not reality. Offering a folder and sharing one are different
        // things, and the difference is the whole reason somebody is looking
        // at this screen.
        //
        // The pronoun goes too. The person's name is on the same card, two
        // lines up; "them" reads oddly next to it, and reads worse when the
        // sentence is about nothing having happened yet.
        function peerNote(shares) {
            if (!shares.length) {
                return 'No folders shared yet.';
            }
            var pick = function (k) {
                return shares.filter(function (s) { return s.kind === k; })
                    .map(function (s) { return s.label; });
            };
            var offered = pick('notaccepted');
            if (offered.length) {
                return 'Offered ' + joinNames(offered) + ' — not accepted yet.';
            }
            var behind = pick('behind');
            if (behind.length) {
                return 'Receiving ' + joinNames(behind) + '.';
            }
            var paused = pick('paused');
            if (paused.length === shares.length) {
                return joinNames(paused) + ' ' + plural(paused.length, 'is', 'are') + ' paused on their computer.';
            }
            var has = pick('complete');
            if (has.length) {
                return 'Has ' + joinNames(has) + '.';
            }
            return 'Sharing ' + joinNames(shares.map(function (s) { return s.label; })) + '.';
        }

        function shareTitle(s) {
            switch (s.kind) {
                case 'complete':    return s.name + ' has all of this folder.';
                case 'behind':      return s.name + ' has ' + s.pct + '% — ' + size(s.needBytes) + ' still to reach them.';
                case 'notaccepted': return s.name + ' has not accepted this folder yet.';
                case 'paused':      return s.name + ' has this folder paused.';
                default:            return s.name + ': not known yet.';
            }
        }

        // Four tones. They drive colour *and* weight: 'good' and 'busy' render
        // as a quiet single line, 'attention' and 'problem' as a real banner.
        // Nothing that is not a sync fact is allowed to reach this line -- not
        // the GUI-password notice, not a peer running a newer build.
        var TONE = { good: 'good', busy: 'busy', attention: 'attention', problem: 'problem' };

        function headline(folders, peers, errors) {
            var connected = peers.filter(function (p) { return p.connected; });
            var active = folders.filter(function (f) { return !f.paused; });

            // 1. Something is broken.
            if (errors.length) {
                return {
                    tone: TONE.problem,
                    text: errors.length + ' ' + plural(errors.length, 'problem needs', 'problems need') + ' your attention.',
                    detail: 'Open Technical details below to see them.'
                };
            }
            var stopped = folders.filter(function (f) { return f.state === 'stopped'; });
            if (stopped.length) {
                return {
                    tone: TONE.problem,
                    text: stopped.length === 1
                        ? stopped[0].label + ' has stopped.'
                        : stopped.length + ' folders have stopped.',
                    detail: 'Syncthing hit an error it could not work around.'
                };
            }
            var failed = folders.filter(function (f) { return f.state === 'faileditems'; });
            if (failed.length) {
                var n = failed.reduce(function (a, f) { return a + f.failedItems; }, 0);
                return {
                    tone: TONE.problem,
                    text: n + ' ' + plural(n, 'file could not', 'files could not') + ' be saved.',
                    detail: 'Usually a file that is open in another program, or a full disk.'
                };
            }

            // 2. Nothing is broken, but somebody has to decide something.
            var localOnly = folders.filter(function (f) {
                return f.state === 'localadditions' || f.state === 'localunencrypted';
            });
            if (localOnly.length) {
                return {
                    tone: TONE.attention,
                    text: localOnly[0].label + ' has changes that are only on this computer.',
                    detail: 'This folder receives changes; it does not send them. Nobody else can see these.'
                };
            }

            // A share nobody accepted looks exactly like a share at 0% if you
            // only read the percentage, and it is the one of the two that will
            // never fix itself.
            var unaccepted = [];
            active.forEach(function (f) {
                f.people.forEach(function (s) {
                    if (s.kind === 'notaccepted') {
                        unaccepted.push({ folder: f.label, name: s.name });
                    }
                });
            });
            if (unaccepted.length) {
                var names = [];
                unaccepted.forEach(function (u) {
                    if (names.indexOf(u.name) === -1) { names.push(u.name); }
                });
                return {
                    tone: TONE.attention,
                    text: joinNames(names) + ' ' + plural(names.length, 'has', 'have') +
                        ' not accepted ' + unaccepted[0].folder + ' yet.',
                    // No plural() here: both arms were the same string, which
                    // is a decision that never decides anything. Singular they
                    // covers one person and several equally well.
                    detail: 'Until the share is accepted on their own computer, nothing reaches them.'
                };
            }

            if (!folders.length) {
                return {
                    tone: TONE.attention,
                    text: 'No folders yet.',
                    detail: 'Nothing is being synced. Use Setup guide under Actions to get started.'
                };
            }
            if (!active.length) {
                return {
                    tone: TONE.attention,
                    text: 'Everything is paused.',
                    detail: 'No files are being sent or received.'
                };
            }
            var unshared = active.filter(function (f) { return f.state === 'unshared'; });
            if (unshared.length === active.length) {
                return {
                    tone: TONE.attention,
                    text: unshared[0].label + ' is not shared with anybody.',
                    detail: 'Your files are safe here, but nobody else can see them.'
                };
            }

            // 3. Work in progress. Quiet: this is normal, and it finishes on
            // its own.
            var needBytes = folders.reduce(function (a, f) { return a + f.needBytes; }, 0);
            if (needBytes > 0) {
                if (!connected.length) {
                    var who = peers.length === 1 ? peers[0].name : 'the others';
                    return {
                        tone: TONE.busy,
                        text: 'Waiting for ' + who + '.',
                        detail: size(needBytes) + ' still to come, once somebody is online.'
                    };
                }
                return {
                    tone: TONE.busy,
                    text: 'Getting ' + size(needBytes) +
                        (connected.length === 1 ? ' from ' + connected[0].name : '') + '.',
                    detail: 'You can keep working while this happens.'
                };
            }
            var scanning = folders.filter(function (f) {
                return f.state === 'scanning' || f.state === 'scan-waiting';
            });
            if (scanning.length) {
                return {
                    tone: TONE.busy,
                    text: 'Checking ' + scanning[0].label + ' for changes.',
                    detail: 'Looking at what is on this computer. Nothing is being downloaded.'
                };
            }

            // Everything is here, but it has not all reached everybody yet.
            var outbound = 0, waitingOn = [];
            active.forEach(function (f) {
                f.people.forEach(function (s) {
                    if (s.kind === 'behind') {
                        outbound += s.needBytes;
                        if (waitingOn.indexOf(s.name) === -1) { waitingOn.push(s.name); }
                    }
                });
            });
            if (outbound > 0) {
                return {
                    tone: TONE.busy,
                    text: 'Sending ' + size(outbound) + ' to ' + joinNames(waitingOn) + '.',
                    detail: 'Your copy is complete. Theirs is catching up.'
                };
            }

            // 4. Settled, and quiet about it.
            var bytes = active.reduce(function (a, f) { return a + f.localBytes; }, 0);
            var files = active.reduce(function (a, f) { return a + f.localFiles; }, 0);
            var everyone = [];
            active.forEach(function (f) {
                f.people.forEach(function (s) {
                    if (s.kind === 'complete' && everyone.indexOf(s.name) === -1) { everyone.push(s.name); }
                });
            });

            // Settled, but holding files back on purpose. "Everything is
            // here" is false in the way that matters: the files exist, they
            // are listed, and somebody was meant to pick from them. Still
            // quiet -- this is a normal state, not a problem -- but it must
            // not claim to be complete.
            var held = active.reduce(function (a, f) { return a + f.heldBack; }, 0);
            if (held > 0) {
                var heldBytes = active.reduce(function (a, f) { return a + f.heldBackBytes; }, 0);
                return {
                    tone: TONE.good,
                    text: 'Everything you chose is here.',
                    detail: files + ' ' + plural(files, 'file', 'files') + ', ' + size(bytes) +
                        ' — ' + held + ' more ' + plural(held, 'file is', 'files are') +
                        ' available (' + size(heldBytes) + '). Use Choose files to pick ' +
                        plural(held, 'it', 'them') + '.'
                };
            }

            return {
                tone: TONE.good,
                text: 'Everything is here.',
                detail: files + ' ' + plural(files, 'file', 'files') + ', ' + size(bytes) +
                    (everyone.length ? ' — and ' + joinNames(everyone) + ' ' +
                        plural(everyone.length, 'has', 'have') + ' the same' : '')
            };
        }

        // Tolerant by design: a request that fails leaves the previous answer
        // on screen rather than blanking it, because the likeliest cause is
        // Syncthing restarting under us and the next tick will succeed.
        function refresh() {
            var cfg = null, conns = {}, statuses = {}, myID = '';

            var jobs = [
                $http.get(urlbase + '/config').then(function (r) {
                    cfg = r.data || {};
                }, angular.noop),

                // Ask who we are rather than reading it out of the config:
                // config.defaults.device is a template for new devices and its
                // deviceID is empty, so using it silently treats every device
                // in the list as a peer -- including this one.
                $http.get(urlbase + '/system/status').then(function (r) {
                    myID = (r.data && r.data.myID) || '';
                }, angular.noop),

                $http.get(urlbase + '/system/connections').then(function (r) {
                    conns = (r.data && r.data.connections) || {};
                }, angular.noop),

                $http.get(urlbase + '/system/error').then(function (r) {
                    st.errors = ((r.data && r.data.errors) || []).slice();
                }, angular.noop)
            ];

            return $q.all(jobs).then(function () {
                if (!cfg) {
                    return;
                }
                var devices = cfg.devices || [];
                var names = {};
                devices.forEach(function (d) { names[d.deviceID] = d.name || d.deviceID.substring(0, 7); });

                var confirmed = confirmations();
                var folderCfgs = cfg.folders || [];

                var peers = devices
                    .filter(function (d) { return d.deviceID && d.deviceID !== myID; })
                    .map(function (d) {
                        var c = conns[d.deviceID] || {};
                        return {
                            deviceID: d.deviceID,
                            name: names[d.deviceID],
                            connected: !!c.connected,
                            paused: !!d.paused,
                            at: c.at || '',
                            clientVersion: c.clientVersion || '',
                            verified: !!confirmed[normalise(d.deviceID)],
                            // [{label, kind}] -- what is shared with this
                            // person and where each one actually stands.
                            shares: [],
                            note: ''
                        };
                    });
                var peerByID = {};
                peers.forEach(function (p) { peerByID[p.deviceID] = p; });

                // Per-folder status is one request each; per-folder-per-peer
                // completion is one more. This audience has one to three
                // folders and two peers.
                var completions = {};
                var budget = MAX_COMPLETION_REQUESTS;

                var perFolder = folderCfgs.map(function (f) {
                    var sharedIDs = (f.devices || [])
                        .map(function (d) { return d.deviceID; })
                        .filter(function (id) { return id !== myID && peerByID[id]; });

                    var reqs = [
                        $http.get(urlbase + '/db/status?folder=' + encodeURIComponent(f.id))
                            .then(function (r) { statuses[f.id] = r.data || {}; }, angular.noop)
                    ];

                    sharedIDs.forEach(function (id) {
                        if (budget-- <= 0) {
                            return;
                        }
                        reqs.push($http.get(urlbase + '/db/completion?folder=' +
                            encodeURIComponent(f.id) + '&device=' + encodeURIComponent(id))
                            .then(function (r) {
                                (completions[f.id] || (completions[f.id] = {}))[id] = r.data || {};
                            }, angular.noop));
                    });

                    return $q.all(reqs).then(function () {
                        return { cfg: f, sharedIDs: sharedIDs };
                    });
                });

                return $q.all(perFolder).then(function (results) {
                    st.folders = results.map(function (res) {
                        var f = res.cfg;
                        var s = statuses[f.id] || {};
                        var people = res.sharedIDs.map(function (id) {
                            return shareOf(peerByID[id], (completions[f.id] || {})[id]);
                        });
                        people.forEach(function (p) {
                            p.title = shareTitle(p);
                            peerByID[p.deviceID].shares.push({ label: f.label || f.id, kind: p.kind });
                        });

                        // Files in the shared index this computer has
                        // deliberately not taken, via selective sync.
                        //
                        // Only counted once the folder has settled: while it is
                        // still pulling, global > local just means "not
                        // finished yet", which needBytes already says. Without
                        // this the screen reports "1 file, up to date" about a
                        // folder holding twenty-two more that somebody was
                        // meant to choose from -- the same mistake as reading
                        // local state alone and calling it "everything is
                        // here".
                        var settled = s.ignorePatterns && (s.needBytes || 0) === 0;
                        var heldBack = settled
                            ? Math.max(0, (s.globalFiles || 0) - (s.localFiles || 0)) : 0;
                        var heldBackBytes = settled
                            ? Math.max(0, (s.globalBytes || 0) - (s.localBytes || 0)) : 0;

                        return {
                            id: f.id,
                            label: f.label || f.id,
                            path: f.path,
                            type: f.type,
                            paused: !!f.paused,
                            state: folderState(f, s),
                            needBytes: s.needBytes || 0,
                            needFiles: s.needFiles || 0,
                            localBytes: s.localBytes || 0,
                            localFiles: s.localFiles || 0,
                            globalBytes: s.globalBytes || 0,
                            globalFiles: s.globalFiles || 0,
                            failedItems: s.errors || 0,
                            hasIgnores: !!s.ignorePatterns,
                            heldBack: heldBack,
                            heldBackBytes: heldBackBytes,
                            // Who this folder is shared with, and where each of
                            // them has actually got to.
                            people: people,
                            peopleNote: peopleNote(people, heldBack)
                        };
                    });

                    refreshReclaimable(st.folders);

                    peers.forEach(function (p) { p.note = peerNote(p.shares); });
                    st.peers = peers;
                    st.headline = headline(st.folders, st.peers, st.errors);
                    st.ready = true;
                });
            });
        }

        // Ask the server to open a folder in Explorer. The browser cannot: a
        // file:// link from an http:// page is blocked, so the path on the card
        // is otherwise something to copy out by hand. See lib/api/api_reveal.go.
        //
        // Resolves to '' when the window has been opened and to a sentence when
        // it has not -- a folder on a drive that is not plugged in is the case
        // that actually happens, and it is worth saying out loud rather than
        // leaving a button that does nothing when pressed.
        // ------------------------------------------------------- reclaiming
        //
        // /rest/db/reclaimable walks the global index and stats every ignored
        // file, so it is emphatically not something to hang off the 2.5 second
        // poll. Two things keep it cheap:
        //
        //   - it is only asked at all for a folder whose /rest/db/status says
        //     ignorePatterns, which is the folders that have been through the
        //     picker and no others;
        //   - and then at most once a minute. The number it returns changes
        //     when somebody re-picks, which is a thing a person does every few
        //     weeks, not every few seconds.
        //
        // markReclaimStale() is how the picker and a completed reclaim ask for
        // an immediate answer rather than waiting out the interval.
        var RECLAIM_MS = 60000;
        var reclaimAsked = {};

        function refreshReclaimable(folders) {
            var now = Date.now();
            folders.forEach(function (f) {
                if (!f.hasIgnores) {
                    delete st.reclaimable[f.id];
                    return;
                }
                if (reclaimAsked[f.id] && now - reclaimAsked[f.id] < RECLAIM_MS) {
                    return;
                }
                reclaimAsked[f.id] = now;
                $http.get(urlbase + '/db/reclaimable', { params: { folder: f.id } })
                    .then(function (r) {
                        if (r.data && r.data.bytes > 0) {
                            st.reclaimable[f.id] = r.data;
                        } else {
                            delete st.reclaimable[f.id];
                        }
                    })
                    .catch(function () {
                        // Leave whatever was there. A failed probe is not
                        // evidence that nothing is reclaimable, and blanking
                        // the row would make the button flicker.
                    });
            });
        }

        function markReclaimStale(folderID) {
            if (folderID) {
                delete reclaimAsked[folderID];
            } else {
                reclaimAsked = {};
            }
        }

        // reclaim deletes. The server re-runs every rail itself; expectBytes is
        // the one thing it takes on trust from here, and it takes it in order
        // to *refuse* -- if the folder moved underneath the confirmation, the
        // numbers the person agreed to no longer describe the set, and 409 is
        // a better outcome than deleting a different one.
        function reclaim(folderID, expectBytes) {
            return $http.post(urlbase + '/db/reclaim', {
                folder: folderID,
                expectBytes: expectBytes
            }).then(function (r) {
                markReclaimStale(folderID);
                delete st.reclaimable[folderID];
                return { ok: true, result: r.data };
            }, function (r) {
                if (r && r.status === 409) {
                    markReclaimStale(folderID);
                    return { ok: false, message: 'This folder changed while you were deciding, so nothing was deleted. Have another look.' };
                }
                return { ok: false, message: 'Could not delete those copies.' };
            });
        }

        function reveal(folderID) {
            return $http.post(urlbase + '/system/reveal?folder=' +
                encodeURIComponent(folderID)).then(function () {
                return '';
            }, function (r) {
                if (r && r.status === 404) {
                    return 'That folder is not on this computer right now — check the drive it lives on.';
                }
                if (r && r.status === 501) {
                    return 'Opening a folder only works on Windows.';
                }
                return 'Could not open the folder.';
            });
        }

        return {
            state: st,
            refresh: refresh,
            reveal: reveal,
            reclaim: reclaim,
            markReclaimStale: markReclaimStale,
            pollMs: POLL_MS,
            // Exported for custom/scripts/test-home-render.js, which asserts
            // the headline rules directly without standing up Angular.
            _headline: headline,
            _size: size,
            _shareOf: shareOf,
            _initials: initials,
            _peopleNote: peopleNote,
            _peerNote: peerNote
        };
    })

    .directive('desuqHome', function (desuqHome, desuqHistory, $timeout) {
        'use strict';

        return {
            restrict: 'E',
            // A child scope, not an isolate one: the template calls the
            // controller's addFolder(), editFolderExisting(), setFolderPause()
            // and friends by name, so every action upstream's markup offers
            // keeps working without reimplementing any of it.
            scope: true,
            templateUrl: 'syncthing/desuq/homeView.html',
            link: function (scope) {
                scope.home = desuqHome.state;

                // The card facts are sentences, so they use the same formatter
                // as the headline rather than upstream's binary/decimal toggle.
                scope.bytes = desuqHome._size;

                // Keyed by folder ID rather than held on the folder object,
                // because refresh() rebuilds that array every 2.5 seconds and
                // would take the message with it.
                scope.revealError = {};

                // The history screen is a sibling element, not a child, so the
                // two share state through the service rather than a scope. The
                // template hides this whole screen while that one is open.
                scope.hist = desuqHistory.state;
                scope.openHistory = function (folderID, tab) {
                    desuqHistory.open(folderID, tab);
                };

                // Which card has its reclaim confirmation open, and what the
                // last attempt said. Keyed by folder id for the same reason
                // revealError is: refresh() rebuilds the folder array every
                // 2.5 seconds and would take anything held on it away.
                scope.reclaimOpen = {};
                scope.reclaimNote = {};

                scope.toggleReclaim = function (folderID) {
                    scope.reclaimOpen[folderID] = !scope.reclaimOpen[folderID];
                    scope.reclaimNote[folderID] = '';
                };

                scope.doReclaim = function (folderID, expectBytes) {
                    scope.reclaimBusy = folderID;
                    desuqHome.reclaim(folderID, expectBytes).then(function (out) {
                        scope.reclaimBusy = '';
                        scope.reclaimOpen[folderID] = false;
                        if (!out.ok) {
                            scope.reclaimNote[folderID] = out.message;
                            return;
                        }
                        var r = out.result || {};
                        var msg = 'Deleted ' + r.files + ' ' +
                            (r.files === 1 ? 'file' : 'files') + ', ' +
                            desuqHome._size(r.bytes) + ' freed.';
                        if (r.keptTotal) {
                            msg += ' ' + r.keptTotal + ' ' +
                                (r.keptTotal === 1 ? 'file was' : 'files were') +
                                ' kept — see below.';
                        }
                        scope.reclaimNote[folderID] = msg;
                        scope.reclaimKept[folderID] = r.kept || [];
                    });
                };
                scope.reclaimKept = {};

                scope.reveal = function (folderID) {
                    desuqHome.reveal(folderID).then(function (msg) {
                        scope.revealError[folderID] = msg;
                    });
                };

                // The picker says so when it rewrites a folder's ignores; the
                // probe is throttled to once a minute otherwise, and waiting
                // that long to notice your own click is the wrong answer.
                scope.$on('desuq:ignoresChanged', function (_, folderID) {
                    desuqHome.markReclaimStale(folderID);
                });

                var poller = null;
                var dead = false;

                function tick() {
                    desuqHome.refresh()['finally'](function () {
                        if (dead) {
                            return;
                        }
                        poller = $timeout(tick, desuqHome.pollMs);
                    });
                }
                tick();

                scope.$on('$destroy', function () {
                    dead = true;
                    if (poller) {
                        $timeout.cancel(poller);
                    }
                });
            }
        };
    });
