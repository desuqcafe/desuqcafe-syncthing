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
            // folder id -> { count, bytes } from /rest/folder/conflicts. Only
            // folders with something in conflict appear.
            conflicts: {},
            // folder id -> the dry run from /rest/db/reclaimable, only for
            // folders that have something to reclaim.
            reclaimable: {},
            // /rest/system/tray: whether the notification-area app is still
            // on disk. null until the first answer.
            tray: null,
            // folder id -> { rows, canClaim, reason, files, bytes } from
            // /rest/folder/claims: who is working on what, and how much of
            // the folder's file count is the claims themselves.
            claims: {},
            // folder id -> { rows, canNote, reason } from /rest/folder/notes:
            // why files changed, in the words of whoever changed them.
            notes: {},
            // folder id -> device id -> { files, bytes } from
            // /rest/db/peerheldback: what each peer is choosing not to keep.
            peerHeld: {},
            // folder id -> device id -> { yours, names } from
            // /rest/db/delivery: which of this computer's changes each
            // person does not have yet.
            delivery: {},
            // folder id -> { through, via } from /rest/db/hub: who in the
            // folder only reaches whom through one computer.
            hub: {},
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
        function shareOf(peer, completion, held, delivery) {
            var c = completion || {};
            var h = held || {};
            var dv = delivery || {};
            var pct = typeof c.completion === 'number' ? c.completion : null;
            var kind;
            // 'unknown' from a *connected* peer is also "not accepted", and it
            // is the common case. The server records a peer's folder states
            // only when their cluster config arrives -- at connect, and when
            // their config changes. Share a new folder with somebody already
            // connected and their last config predates it; a pending offer
            // changes nothing on their side, so nothing new is sent, and the
            // state stays 'unknown' until they accept or reconnect. Accepting
            // *does* send one, so while connected, 'unknown' cannot mean they
            // have it. Disconnected, the states are dropped and it means
            // nothing at all -- left to the percentage below.
            if (c.remoteState === 'notSharing' ||
                (c.remoteState === 'unknown' && peer.connected)) {
                kind = 'notaccepted';
            } else if (c.remoteState === 'paused' || peer.paused) {
                kind = 'paused';
            } else if ((h.changed || 0) > 0) {
                // Changed on their own computer, in a folder that only
                // receives there. Completion calls this "behind" -- they need
                // the shared version of every file they changed -- and it
                // is not: nothing is on its way, and nothing will be until
                // they undo it. Verified on a pair: one edit and one deletion
                // read as 60%, two items needed, remote state valid.
                kind = 'changedthere';
            } else if (pct === null) {
                kind = 'unknown';
            } else if (pct >= 100 && (h.files || 0) > 0) {
                // Complete by completion's measure, and holding files back by
                // their own index: an ignored file is not needed, so a peer
                // who took one texture out of six reads 100%. Their index
                // says which files they marked invalid, which is what
                // ignoring does (lib/model/desuq_peerheldback.go).
                kind = 'partial';
            } else if (pct >= 100) {
                kind = 'complete';
            } else if (!peer.connected) {
                // Behind, and not connected: nothing is moving. Completion is
                // computed from their stored index and reads the same whether
                // they are here or not, so this said "still catching up, 12
                // MiB to go" about a computer switched off since Tuesday --
                // and the headline said "Sending 12 MiB to Kai". Delivery
                // (lib/api/api_delivery.go) says which of it is yours.
                kind = 'away';
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
                needBytes: c.needBytes || 0,
                heldFiles: h.files || 0,
                heldBytes: h.bytes || 0,
                changedFiles: h.changed || 0,
                // Of what they are missing, the part this computer made.
                // Only meaningful while they are behind or away; a delivery
                // answer from before they caught up is ignored.
                yoursFiles: (kind === 'behind' || kind === 'away') ? ((dv.yours && dv.yours.files) || 0) : 0,
                yoursNames: (kind === 'behind' || kind === 'away')
                    ? (dv.names || []).map(function (n) { return baseName(n); }) : []
            };
        }

        // The last part of an index path, which carries the OS separator.
        function baseName(path) {
            var parts = String(path || '').split(/[\\\/]/);
            return parts[parts.length - 1] || String(path || '');
        }

        // "cabin.blend", "cabin.blend and tree.blend", "cabin.blend and 4
        // more": the files a delivery is about, named, because "3 files" is
        // not the thing anybody is waiting for.
        function yoursPhrase(s) {
            var names = s.yoursNames || [];
            if (!s.yoursFiles || !names.length) {
                return s.yoursFiles + ' ' + plural(s.yoursFiles, 'file', 'files');
            }
            if (s.yoursFiles === 1) {
                return names[0];
            }
            if (s.yoursFiles === 2 && names.length >= 2) {
                return names[0] + ' and ' + names[1];
            }
            return names[0] + ' and ' + (s.yoursFiles - 1) + ' more';
        }

        // One sentence under the people strip. The strip says who; this says
        // what it means, because a row of coloured initials is a legend nobody
        // was given.
        function peopleNote(people, heldBack, localFiles) {
            if (!people.length) {
                return 'Not shared with anybody yet.';
            }
            // "has the same files as you" is a claim about both sides, and it
            // is false the moment selective sync is in play: they have the
            // whole folder, this computer has the part somebody picked. Say
            // what is actually true instead, and in the direction that
            // matters -- their copy is the complete one.
            // The same is true the other way round: a peer can hold files back
            // too, and completion reads 100% for them all the same. Both
            // directions are said, and neither is ever "the same files".
            var whole = people.filter(function (s) { return s.kind === 'complete'; })
                .map(function (s) { return s.name; });
            var part = people.filter(function (s) { return s.kind === 'partial'; });
            var partNames = part.map(function (s) { return s.name; });
            var partSentence = '';
            if (part.length === 1) {
                partSentence = part[0].name + ' keeps only part of it — ' + part[0].heldFiles + ' ' +
                    plural(part[0].heldFiles, 'file is', 'files are') + ' not on their computer (' +
                    size(part[0].heldBytes) + ').';
            } else if (part.length) {
                partSentence = joinNames(partNames) + ' keep only part of it.';
            }
            var settledAll = whole.length + part.length === people.length;

            if (heldBack > 0 && settledAll) {
                // "Part of it" over nothing at all is the picker closed
                // without a choice; say that instead.
                var you = localFiles === 0
                    ? 'You have not picked anything from it yet.'
                    : 'You have chosen part of it.';
                return [
                    whole.length ? joinNames(whole) + ' ' + plural(whole.length, 'has', 'have') + ' the whole folder.' : '',
                    partSentence,
                    you
                ].filter(Boolean).join(' ');
            }
            if (settledAll && part.length) {
                return [
                    partSentence,
                    whole.length ? joinNames(whole) + ' ' + plural(whole.length, 'has', 'have') +
                        ' the same files as you.' : ''
                ].filter(Boolean).join(' ');
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
            var own = people.filter(function (s) { return s.kind === 'changedthere'; });
            if (own.length) {
                return own.map(function (s) {
                    return s.name + ' changed ' + s.changedFiles + ' ' + plural(s.changedFiles, 'file', 'files') +
                        ' on their own computer. Their copy only receives, so those changes stay with them.';
                }).join(' ');
            }
            var behind = people.filter(function (s) { return s.kind === 'behind'; });
            var away = people.filter(function (s) { return s.kind === 'away'; });
            if (behind.length || away.length) {
                // Yours first: "your latest has not reached Kai" is the
                // sentence a person acts on -- wait before switching off,
                // or tell them. Somebody else's changes still on their way
                // to a third person is background.
                var sentences = [];
                behind.forEach(function (s) {
                    if (s.yoursFiles > 0) {
                        sentences.push('Sending ' + yoursPhrase(s) + ' to ' + s.name + '.');
                    }
                });
                var others = behind.filter(function (s) { return !(s.yoursFiles > 0); });
                if (others.length) {
                    var pending = others.reduce(function (a, s) { return a + s.needBytes; }, 0);
                    sentences.push(joinNames(others.map(function (s) { return s.name; })) + ' ' +
                        plural(others.length, 'is', 'are') + ' still catching up' +
                        (pending > 0 ? ' — ' + size(pending) + ' to go.' : '.'));
                }
                away.forEach(function (s) {
                    sentences.push(s.yoursFiles > 0
                        ? s.name + ' does not have your latest ' + yoursPhrase(s) +
                            ' yet — their computer is not connected. It goes when they are back.'
                        : s.name + ' is not connected. ' + size(s.needBytes) +
                            ' of changes will reach them when they are back.');
                });
                return sentences.join(' ');
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
            var own = pick('changedthere');
            if (own.length) {
                return 'Has changes in ' + joinNames(own) + ' that stay on their computer.';
            }
            var behind = pick('behind');
            if (behind.length) {
                return 'Receiving ' + joinNames(behind) + '.';
            }
            var away = pick('away');
            if (away.length) {
                return 'Not connected — ' + joinNames(away) + ' will catch up when they are back.';
            }
            var paused = pick('paused');
            if (paused.length === shares.length) {
                return joinNames(paused) + ' ' + plural(paused.length, 'is', 'are') + ' paused on their computer.';
            }
            var has = pick('complete');
            var part = pick('partial');
            if (has.length && part.length) {
                return 'Has ' + joinNames(has) + ', and part of ' + joinNames(part) + '.';
            }
            if (has.length) {
                return 'Has ' + joinNames(has) + '.';
            }
            if (part.length) {
                return 'Has part of ' + joinNames(part) + '.';
            }
            return 'Sharing ' + joinNames(shares.map(function (s) { return s.label; })) + '.';
        }

        function shareTitle(s) {
            switch (s.kind) {
                case 'complete':    return s.name + ' has all of this folder.';
                case 'partial':     return s.name + ' keeps part of this folder — ' + s.heldFiles + ' ' +
                    plural(s.heldFiles, 'file', 'files') + ' (' + size(s.heldBytes) + ') are not on their computer.';
                case 'behind':      return s.name + ' has ' + s.pct + '% — ' + size(s.needBytes) + ' still to reach them.';
                case 'away':        return s.name + ' is not connected. ' + (s.yoursFiles > 0
                    ? 'Your latest ' + yoursPhrase(s) + ' has not reached them yet.'
                    : size(s.needBytes) + ' will reach them when they are back.');
                case 'changedthere': return s.name + ' changed ' + s.changedFiles + ' ' + plural(s.changedFiles, 'file', 'files') +
                    ' on their computer. Their copy only receives, so the changes stay there.';
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

        function headline(folders, peers, errors, tray) {
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
            // Windows Security has removed the tray (DEPLOYMENT-3D-TEAM.md
            // section 20). This IS a sync fact, and the most important one on
            // the screen: the tray is what starts Syncthing at sign-in, so
            // everything below stays true until the next restart and then
            // nothing syncs at all. It outranks a stopped folder because a
            // stopped folder says so every time you look, and this will not.
            if (tray && tray.expected && !tray.present) {
                return {
                    tone: TONE.problem,
                    text: 'Windows Security removed part of this app. Syncing will stop after you restart this computer.',
                    detail: 'It is a false alarm. To put it back: open Windows Security, go to ' +
                        'Virus & threat protection, then Protection history, find the entry that ' +
                        'mentions desuq-syncthing-tray, and choose Restore. If you are not sure, ' +
                        'ask whoever set this up for you before restarting.'
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
                // The second sentence is the trap. Switching a receive-only
                // folder to send-and-receive publishes everything it was
                // holding -- deletions included. Verified on a pair: a file
                // deleted on the receive-only side was deleted for everybody
                // the moment the folder type changed.
                return {
                    tone: TONE.attention,
                    text: localOnly[0].label + ' has changes that are only on this computer.',
                    detail: 'This folder receives changes; it does not send them. Nobody else can see these. ' +
                        'Do not switch it to Send & Receive to share them: that sends every one, ' +
                        'deletions included. Undo my changes here puts back the shared version.'
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
            // Only people who are connected: 'away' is behind with nothing
            // moving, and "Sending" about a switched-off computer was false.
            var outbound = 0, waitingOn = [], yours = [];
            active.forEach(function (f) {
                f.people.forEach(function (s) {
                    if (s.kind === 'behind') {
                        outbound += s.needBytes;
                        if (waitingOn.indexOf(s.name) === -1) { waitingOn.push(s.name); }
                        if (s.yoursFiles > 0) { yours.push(yoursPhrase(s) + ' to ' + s.name); }
                    }
                });
            });
            if (outbound > 0) {
                return {
                    tone: TONE.busy,
                    text: 'Sending ' + size(outbound) + ' to ' + joinNames(waitingOn) + '.',
                    detail: yours.length
                        ? 'On its way: ' + yours.join('; ') + '. Your copy is complete.'
                        : 'Your copy is complete. Theirs is catching up.'
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
                // Nothing chosen at all. "Everything you chose is here" over
                // zero files is true only in the way that misleads, and the
                // way to get here is closing the picker without choosing:
                // dismissed() holds everything back and pauses, and Resume
                // then runs a folder that fetches nothing. Somebody meant to
                // pick and has not, so it is their move -- attention, not the
                // quiet line.
                if (files === 0) {
                    return {
                        tone: TONE.attention,
                        text: 'Nothing has been picked yet.',
                        detail: held + ' ' + plural(held, 'file is', 'files are') + ' available (' +
                            size(heldBytes) + ') and ' + (held === 1 ? 'it is not' : 'none are') +
                            ' on this computer yet. Use Choose files to pick what you want here.'
                    };
                }
                return {
                    tone: TONE.good,
                    text: 'Everything you chose is here.',
                    detail: files + ' ' + plural(files, 'file', 'files') + ', ' + size(bytes) +
                        ' — ' + held + ' more ' + plural(held, 'file is', 'files are') +
                        ' available (' + size(heldBytes) + '). Use Choose files to pick ' +
                        plural(held, 'it', 'them') + '.'
                };
            }

            // Somebody who keeps only part of a folder does not "have the
            // same", however complete completion says they are.
            var partly = [], ownChanges = [], undelivered = [], offline = [];
            active.forEach(function (f) {
                f.people.forEach(function (s) {
                    if (s.kind === 'partial' && partly.indexOf(s.name) === -1) { partly.push(s.name); }
                    if (s.kind === 'changedthere' && ownChanges.indexOf(s.name) === -1) { ownChanges.push(s.name); }
                    if (s.kind === 'away') {
                        var into = s.yoursFiles > 0 ? undelivered : offline;
                        if (into.indexOf(s.name) === -1) { into.push(s.name); }
                    }
                });
            });
            offline = offline.filter(function (n) { return undelivered.indexOf(n) === -1; });
            everyone = everyone.filter(function (n) { return partly.indexOf(n) === -1; });
            var others = [];
            if (everyone.length) {
                others.push(joinNames(everyone) + ' ' + plural(everyone.length, 'has', 'have') + ' the same');
            }
            if (partly.length) {
                others.push(joinNames(partly) + ' ' + plural(partly.length, 'keeps', 'keep') + ' only part of it');
            }
            if (ownChanges.length) {
                others.push(joinNames(ownChanges) + ' ' + plural(ownChanges.length, 'has', 'have') +
                    ' changes that stay on their computer');
            }
            // Still "Everything is here": it is, on this computer. What has
            // not happened is the other half, and it happens by itself when
            // they connect -- a normal evening, not a problem.
            if (undelivered.length) {
                others.push(joinNames(undelivered) + ' ' + plural(undelivered.length, 'does', 'do') +
                    ' not have your latest yet — not connected');
            }
            if (offline.length) {
                others.push(joinNames(offline) + ' ' + plural(offline.length, 'is', 'are') +
                    ' not connected, and will catch up when back');
            }
            return {
                tone: TONE.good,
                text: 'Everything is here.',
                detail: files + ' ' + plural(files, 'file', 'files') + ', ' + size(bytes) +
                    (others.length ? ' — ' + others.join('; ') : '')
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
                            return shareOf(peerByID[id], (completions[f.id] || {})[id],
                                (st.peerHeld[f.id] || {})[id], (st.delivery[f.id] || {})[id]);
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

                        // The claims files (lib/api/api_claims.go) are in the
                        // folder like anything else, so Syncthing counts them.
                        // They are bookkeeping, not somebody's work: "1 of 1
                        // files chosen" about a folder where nothing was
                        // picked and Yuki's claim arrived would be the local
                        // state lying again. They are on both sides of the
                        // count, so heldBack is unchanged.
                        var fc = st.claims[f.id] || {};
                        var claimFiles = fc.files || 0, claimBytes = fc.bytes || 0;
                        var localFiles = Math.max(0, (s.localFiles || 0) - claimFiles);
                        var localBytes = Math.max(0, (s.localBytes || 0) - claimBytes);

                        return {
                            id: f.id,
                            label: f.label || f.id,
                            path: f.path,
                            type: f.type,
                            paused: !!f.paused,
                            state: folderState(f, s),
                            needBytes: s.needBytes || 0,
                            needFiles: s.needFiles || 0,
                            localBytes: localBytes,
                            localFiles: localFiles,
                            globalBytes: Math.max(0, (s.globalBytes || 0) - claimBytes),
                            globalFiles: Math.max(0, (s.globalFiles || 0) - claimFiles),
                            // Who is working on what here, others first:
                            // theirs is what you need to know before opening
                            // anything, yours is only a reminder.
                            claims: (fc.rows || []).slice().sort(function (a, b) {
                                return (a.mine === b.mine) ? 0 : (a.mine ? 1 : -1);
                            }),
                            canClaim: fc.canClaim !== false,
                            claimReason: fc.reason || '',
                            // Why files changed (lib/api/api_notes.go): only
                            // notes about files as they are now, and recent.
                            // A note about a version since replaced belongs
                            // to History, where that version has a row.
                            notes: cardNotes((st.notes[f.id] || {}).rows, Date.now()),
                            canNote: (st.notes[f.id] || {}).canNote !== false,
                            failedItems: s.errors || 0,
                            // Why a stopped folder stopped. Upstream puts the
                            // sentence in the summary and nothing showed it,
                            // so "Stopped" was the whole of what the screen
                            // knew how to say.
                            error: s.error || '',
                            hasIgnores: !!s.ignorePatterns,
                            heldBack: heldBack,
                            heldBackBytes: heldBackBytes,
                            // Who this folder is shared with, and where each of
                            // them has actually got to.
                            people: people,
                            peopleNote: peopleNote(people, heldBack, localFiles),
                            hub: hubNotes(st.hub[f.id])
                        };
                    });

                    refreshReclaimable(st.folders);
                    refreshConflicts();
                    refreshTray();
                    refreshClaims();
                    refreshNotes();
                    refreshPeerHeld(st.folders);
                    refreshDelivery(st.folders);
                    refreshHub(st.folders);

                    peers.forEach(function (p) { p.note = peerNote(p.shares); });
                    st.peers = peers;
                    st.headline = headline(st.folders, st.peers, st.errors, st.tray);
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

        // ------------------------------------------------------- conflicts
        //
        // One request for every folder rather than one per folder: the server
        // walks the global index either way, and the card only wants a count.
        // Same cadence as the reclaim probe and for the same reason -- a
        // conflict is created by somebody else's edit landing, which is not a
        // thing that happens between two ticks of a 2.5 second poll.
        var CONFLICT_MS = 60000;
        var conflictsAsked = 0;

        function refreshConflicts() {
            var now = Date.now();
            if (conflictsAsked && now - conflictsAsked < CONFLICT_MS) {
                return;
            }
            conflictsAsked = now;
            $http.get(urlbase + '/folder/conflicts')
                .then(function (r) {
                    var next = {};
                    ((r.data && r.data.folders) || []).forEach(function (f) {
                        if (f.count > 0) {
                            next[f.folder] = { count: f.count, bytes: f.bytes };
                        }
                    });
                    st.conflicts = next;
                })
                .catch(function () {
                    // Leave what is there. A failed probe is not evidence that
                    // a conflict went away, and blanking the row would make it
                    // flicker.
                });
        }

        function markConflictsStale() {
            conflictsAsked = 0;
        }

        // ------------------------------------------------- what peers keep
        //
        // Completion reads 100% for a peer who took one file of six, because
        // an ignored file is not needed. /rest/db/peerheldback reads their own
        // index instead, which walks it -- so only for people who look
        // complete, at most once a minute, within the same cap as completion.
        var PEER_HELD_MS = 60000;
        var peerHeldAsked = 0;

        function refreshPeerHeld(folders) {
            var now = Date.now();
            if (peerHeldAsked && now - peerHeldAsked < PEER_HELD_MS) {
                return;
            }
            peerHeldAsked = now;
            var budget = MAX_COMPLETION_REQUESTS;
            folders.forEach(function (f) {
                f.people.forEach(function (s) {
                    // 'behind' too, and only while connected: a peer changing
                    // files in a receive-only copy reads as behind, and their
                    // index is the one place that says otherwise.
                    var worth = s.kind === 'complete' || s.kind === 'partial' ||
                        s.kind === 'changedthere' || (s.kind === 'behind' && s.connected);
                    if (!worth || budget-- <= 0) {
                        return;
                    }
                    $http.get(urlbase + '/db/peerheldback',
                        { params: { folder: f.id, device: s.deviceID } })
                        .then(function (r) {
                            (st.peerHeld[f.id] || (st.peerHeld[f.id] = {}))[s.deviceID] = {
                                files: (r.data && r.data.files) || 0,
                                bytes: (r.data && r.data.bytes) || 0,
                                changed: (r.data && r.data.changed) || 0
                            };
                        }, angular.noop);
                });
            });
        }

        // --------------------------------------------------------- delivery
        //
        // Which of *your* changes each person is missing
        // (lib/api/api_delivery.go). Asked only for folders where somebody
        // is behind or away, because it walks their remote need -- and that
        // is empty, and so free, for everybody who is in step. Faster than
        // the held-back probe: "Sending cabin.blend to Kai" is only worth
        // saying while it is true.
        var DELIVERY_MS = 10000;
        var deliveryAsked = 0;

        function refreshDelivery(folders) {
            var now = Date.now();
            if (deliveryAsked && now - deliveryAsked < DELIVERY_MS) {
                return;
            }
            deliveryAsked = now;
            folders.forEach(function (f) {
                var worth = f.people.some(function (s) { return s.kind === 'behind' || s.kind === 'away'; });
                if (!worth) {
                    delete st.delivery[f.id];
                    return;
                }
                $http.get(urlbase + '/db/delivery', { params: { folder: f.id } })
                    .then(function (r) {
                        var byDevice = {};
                        ((r.data && r.data.peers) || []).forEach(function (p) {
                            byDevice[p.device] = p;
                        });
                        st.delivery[f.id] = byDevice;
                    }, angular.noop);
            });
        }

        // -------------------------------------------------------------- hub
        //
        // Three people and one folder, set up the usual way: the person who
        // made it shared it with each of the other two, and those two were
        // never told about each other. Everything reaches everybody through
        // the one computer in the middle -- until it is off, when the other
        // two stop syncing with each other while both are online, and
        // nothing says so. /rest/db/hub reads who each peer says it shares
        // the folder with (lib/api/api_hub.go). It only changes when
        // somebody's configuration does, so once a minute is plenty.
        var HUB_MS = 60000;
        var hubAsked = 0;

        function refreshHub(folders) {
            var now = Date.now();
            var shared = folders.some(function (f) { return f.people.length > 0; });
            if (!shared || (hubAsked && now - hubAsked < HUB_MS)) {
                return;
            }
            hubAsked = now;
            $http.get(urlbase + '/db/hub').then(function (r) {
                var next = {};
                ((r.data && r.data.folders) || []).forEach(function (f) { next[f.folder] = f; });
                st.hub = next;
            }, angular.noop);
        }

        // hubNotes turns one folder's answer into what the card says.
        //
        // through: this computer is the middle. Said as a fact with its
        // consequence, and what to do about it -- which is on *their*
        // screens, since this computer already shares with both.
        //
        // via: this computer is an edge, and here there is something to
        // press. Named the way the person in the middle named them, since
        // this computer has never met them.
        function hubNotes(h) {
            if (!h) {
                return { through: '', via: [] };
            }
            var through = '';
            var pairs = h.through || [];
            if (pairs.length === 1) {
                through = pairs[0].a.name + ' and ' + pairs[0].b.name + ' only sync with each other through ' +
                    'this computer. When it is off, their changes do not reach each other. ' +
                    'Each of them can fix that with "Connect directly" on their own main screen.';
            } else if (pairs.length > 1) {
                var names = [];
                pairs.forEach(function (p) {
                    [p.a.name, p.b.name].forEach(function (n) {
                        if (names.indexOf(n) === -1) { names.push(n); }
                    });
                });
                through = joinNames(names) + ' only sync with each other through this computer. ' +
                    'When it is off, their changes do not reach each other.';
            }
            var via = (h.via || []).map(function (v) {
                var middle = joinNames((v.via || []).map(function (m) { return m.name; }));
                return {
                    device: v.device,
                    name: v.name,
                    text: 'You get ' + v.name + "'s changes only through " + middle + "'s computer. " +
                        'When it is off, the two of you do not sync.'
                };
            });
            return { through: through, via: via };
        }

        // Add the person as a device and share the folder with them. They
        // are asked to accept, as for anybody new. Answers a sentence either
        // way: success is also something to say, because nothing visible
        // changes until they accept.
        function hubConnect(folderID, device, name) {
            return $http.post(urlbase + '/db/hub/connect', { folder: folderID, device: device })
                .then(function () {
                    hubAsked = 0;
                    return { ok: true, text: 'Asked to connect to ' + name + '. They need to accept on their ' +
                        'computer, and check the verification card with you.' };
                }, function (r) {
                    return { ok: false, text: 'Could not add ' + name + ': ' +
                        ((r && r.data) ? String(r.data).trim() : 'try again in a moment') + '.' };
                });
        }

        // ----------------------------------------------------------- claims
        //
        // "I'm working on this file" (lib/api/api_claims.go). Faster than the
        // other probes: a claim is only worth anything if it is seen before
        // the other person opens the file. Still a handful of small local
        // reads per folder, not a walk.
        var CLAIMS_MS = 10000;
        var claimsAsked = 0;

        function takeClaims(data) {
            var next = {};
            ((data && data.folders) || []).forEach(function (f) {
                next[f.folder] = {
                    rows: [], canClaim: f.canClaim, reason: f.reason || '',
                    files: f.files || 0, bytes: f.bytes || 0
                };
            });
            ((data && data.claims) || []).forEach(function (c) {
                (next[c.folder] || (next[c.folder] = { rows: [], files: 0, bytes: 0 })).rows.push(c);
            });
            return next;
        }

        function refreshClaims() {
            var now = Date.now();
            if (claimsAsked && now - claimsAsked < CLAIMS_MS) {
                return;
            }
            claimsAsked = now;
            $http.get(urlbase + '/folder/claims')
                .then(function (r) { st.claims = takeClaims(r.data); })
                .catch(angular.noop);
        }

        // Done with a file. Answers '' on success and a sentence otherwise;
        // the card is redrawn from the server's answer rather than guessed.
        function releaseClaim(folderID, path) {
            return postClaim({ folder: folderID, path: path, release: true },
                'Could not take that mark off. Try again in a moment.');
        }

        // Hand your mark to somebody else the folder is shared with
        // (lib/api/api_handoff.go). It stays yours, and everybody still sees
        // the file as taken, until their computer picks it up.
        function handClaim(folderID, path, deviceID) {
            return postClaim({ folder: folderID, path: path, handTo: deviceID },
                'Could not hand that file over. Try again in a moment.');
        }

        // Take back a hand-over nobody has picked up yet: marking it by hand
        // again is what does that on the server.
        function keepClaim(folderID, path) {
            return postClaim({ folder: folderID, path: path },
                'Could not keep that file. Try again in a moment.');
        }

        function postClaim(body, failure) {
            return $http.post(urlbase + '/folder/claim', body)
                .then(function (r) {
                    var one = takeClaims(r.data)[body.folder];
                    if (one) {
                        st.claims[body.folder] = one;
                    }
                    claimsAsked = 0;
                    return '';
                }, function (r) {
                    // The server's own sentence when it has one: "that person
                    // does not share this folder" is something to act on.
                    var why = r && r.status >= 400 && r.status < 500 &&
                        typeof r.data === 'string' && r.data.trim();
                    return why ? failure.replace(/ Try again in a moment\.$/, '') + ' ' +
                        why.charAt(0).toUpperCase() + why.slice(1) + '.' : failure;
                });
        }

        // How long ago, in the words a person would use. Coarse on purpose:
        // "since 14:20" today, a weekday within the week, a date beyond.
        function claimSince(iso, stale) {
            var t = new Date(iso);
            if (isNaN(t.getTime())) {
                return '';
            }
            var now = new Date();
            var days = Math.floor((now - t) / 86400000);
            var hhmm = ('0' + t.getHours()).slice(-2) + ':' + ('0' + t.getMinutes()).slice(-2);
            var out;
            if (t.toDateString() === now.toDateString()) {
                out = 'since ' + hhmm;
            } else if (days < 6) {
                out = 'since ' + ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'][t.getDay()];
            } else {
                out = 'since ' + t.toLocaleDateString();
            }
            if (stale) {
                out += ' — ' + days + ' days, may have been forgotten';
            }
            return out;
        }

        // Who one of your own marks has not reached, as a sentence, or ''.
        // A connected peer still pulling it is left out: that is a second's
        // wait and would flash on and off with every mark. Offline is the
        // case the mark was made for -- it used to toast success and reach
        // nobody -- and "not taking marks" never fixes itself.
        function claimUnseen(unseen) {
            var offline = [], held = [];
            (unseen || []).forEach(function (p) {
                if (p.state === 'offline') { offline.push(p.name); }
                if (p.state === 'heldBack') { held.push(p.name); }
            });
            var out = [];
            if (offline.length) {
                out.push(joinNames(offline) + ' ' + plural(offline.length, 'is', 'are') +
                    ' offline and will see this when they reconnect.');
            }
            if (held.length) {
                out.push(joinNames(held) + ' cannot see marks until their copy is updated.');
            }
            return out.join(' ');
        }

        // ------------------------------------------------------------ notes
        //
        // "Why I changed this" (lib/api/api_notes.go). Slower than the marks:
        // a mark is only useful if it is seen before somebody opens the file,
        // a note is read afterwards.
        var NOTES_MS = 30000;
        var NOTES_CARD_DAYS = 14;
        var NOTES_CARD_MAX = 5;
        var notesAsked = 0;

        function takeNotes(data) {
            var next = {};
            ((data && data.folders) || []).forEach(function (f) {
                next[f.folder] = { rows: [], canNote: f.canNote, reason: f.reason || '' };
            });
            ((data && data.notes) || []).forEach(function (n) {
                (next[n.folder] || (next[n.folder] = { rows: [] })).rows.push(n);
            });
            return next;
        }

        function refreshNotes() {
            var now = Date.now();
            if (notesAsked && now - notesAsked < NOTES_MS) {
                return;
            }
            notesAsked = now;
            $http.get(urlbase + '/folder/notes')
                .then(function (r) { st.notes = takeNotes(r.data); })
                .catch(angular.noop);
        }

        // What the card shows: current notes from the last two weeks, newest
        // first, capped. `more` counts what the cap left out, so the card can
        // say so rather than quietly showing five of twelve.
        function cardNotes(rows, now) {
            var cutoff = now - NOTES_CARD_DAYS * 86400000;
            var list = (rows || []).filter(function (n) {
                return n.current && new Date(n.at).getTime() >= cutoff;
            });
            list.sort(function (a, b) { return new Date(b.at) - new Date(a.at); });
            var shown = list.slice(0, NOTES_CARD_MAX);
            shown.more = list.length - shown.length;
            return shown;
        }

        // Your own recent saves in one folder, for the "say why" form.
        function noteChoices(folderID) {
            return $http.get(urlbase + '/folder/notes', { params: { folder: folderID, yours: 1 } })
                .then(function (r) {
                    var one = takeNotes(r.data)[folderID];
                    if (one) {
                        st.notes[folderID] = one;
                    }
                    return (r.data && r.data.yours) || [];
                }, function () { return []; });
        }

        // Write, change or (with empty text) remove a note. Answers '' or a
        // sentence, like postClaim. version is { modified, size } to change a
        // note about an older version; omitted, the note is about the file as
        // it is now.
        function saveNote(folderID, path, text, version) {
            var body = { folder: folderID, path: path, text: text || '' };
            if (version) {
                body.modified = version.modified;
                body.size = version.size;
            }
            return $http.post(urlbase + '/folder/note', body)
                .then(function (r) {
                    var one = takeNotes(r.data)[folderID];
                    if (one) {
                        st.notes[folderID] = one;
                    }
                    notesAsked = 0;
                    return '';
                }, function (r) {
                    var why = r && r.status >= 400 && r.status < 500 &&
                        typeof r.data === 'string' && r.data.trim();
                    return why ? 'Could not save that note. ' +
                        why.charAt(0).toUpperCase() + why.slice(1) + '.'
                        : 'Could not save that note. Try again in a moment.';
                });
        }

        // "2 h ago", "yesterday", a date: when a note was written.
        function noteWhen(iso, now) {
            var t = new Date(iso);
            if (isNaN(t.getTime())) {
                return '';
            }
            now = now || new Date();
            var mins = Math.floor((now - t) / 60000);
            if (mins < 1) {
                return 'just now';
            }
            if (mins < 60) {
                return mins + ' min ago';
            }
            if (mins < 24 * 60) {
                return Math.floor(mins / 60) + ' h ago';
            }
            var y = new Date(now.getTime());
            y.setDate(y.getDate() - 1);
            if (t.toDateString() === y.toDateString()) {
                return 'yesterday';
            }
            return t.toLocaleDateString();
        }

        // ------------------------------------------------------------- tray
        //
        // Whether the notification-area app is still on disk. A stat on the
        // server, so cheap, but it changes when Defender acts or somebody
        // restores it, not between ticks -- the conflicts cadence is plenty.
        // A failed probe keeps the last answer, for the same reason as there.
        var TRAY_MS = 60000;
        var trayAsked = 0;

        function refreshTray() {
            var now = Date.now();
            if (trayAsked && now - trayAsked < TRAY_MS) {
                return;
            }
            trayAsked = now;
            $http.get(urlbase + '/system/tray')
                .then(function (r) { st.tray = r.data || null; })
                .catch(angular.noop);
        }

        // ------------------------------------------------------------ repair
        //
        // The server holds every rail -- see lib/api/api_repair.go, which will
        // not create an empty directory for a folder whose index still holds
        // files. This only carries the answer back.
        function repair(folderID) {
            return $http.post(urlbase + '/folder/repair', { folder: folderID })
                .then(function (r) {
                    return { ok: true, message: (r.data && r.data.message) || 'Done.' };
                }, function (r) {
                    if (r && r.status === 409) {
                        return { ok: false, message: String(r.data || '').trim() };
                    }
                    return { ok: false, message: 'Could not set that folder up again.' };
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
            repair: repair,
            releaseClaim: releaseClaim,
            handClaim: handClaim,
            keepClaim: keepClaim,
            hubConnect: hubConnect,
            claimSince: claimSince,
            claimUnseen: claimUnseen,
            noteChoices: noteChoices,
            saveNote: saveNote,
            noteWhen: noteWhen,
            _cardNotes: cardNotes,
            markReclaimStale: markReclaimStale,
            markConflictsStale: markConflictsStale,
            pollMs: POLL_MS,
            // Exported for custom/scripts/test-home-render.js, which asserts
            // the headline rules directly without standing up Angular.
            _headline: headline,
            _size: size,
            _shareOf: shareOf,
            _initials: initials,
            _peopleNote: peopleNote,
            _peerNote: peerNote,
            _yoursPhrase: yoursPhrase,
            _hubNotes: hubNotes
        };
    })

    .directive('desuqHome', function (desuqHome, desuqHistory, $timeout, $window) {
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

                scope.repairNote = {};
                scope.repairBusy = '';
                scope.doRepair = function (folderID) {
                    scope.repairBusy = folderID;
                    scope.repairNote[folderID] = '';
                    desuqHome.repair(folderID).then(function (out) {
                        scope.repairBusy = '';
                        scope.repairNote[folderID] = out.message;
                        if (out.ok) {
                            desuqHome.refresh();
                        }
                    });
                };

                scope.reveal = function (folderID) {
                    desuqHome.reveal(folderID).then(function (msg) {
                        scope.revealError[folderID] = msg;
                    });
                };

                // Who is working on what. Keyed by folder, like revealError.
                scope.claimSince = desuqHome.claimSince;
                scope.claimUnseen = desuqHome.claimUnseen;

                scope.claimError = {};
                scope.claimHelp = {};

                // "Connect directly", keyed by folder + device.
                scope.hubResult = {};
                scope.hubConnect = function (folderID, v) {
                    var key = folderID + ' ' + v.device;
                    scope.hubResult[key] = { busy: true, text: '' };
                    desuqHome.hubConnect(folderID, v.device, v.name).then(function (res) {
                        scope.hubResult[key] = res;
                    });
                };
                // "Hand over", keyed by folder + path: whether the list of
                // people is open. Only needed with more than one person; with
                // one, the button names them and hands over directly.
                scope.handOpen = {};
                scope.toggleHand = function (folderID, path) {
                    var key = folderID + ' ' + path;
                    scope.handOpen[key] = !scope.handOpen[key];
                };
                scope.handClaim = function (folderID, path, deviceID) {
                    delete scope.handOpen[folderID + ' ' + path];
                    desuqHome.handClaim(folderID, path, deviceID).then(function (msg) {
                        scope.claimError[folderID] = msg;
                        if (!msg) {
                            desuqHome.refresh();
                        }
                    });
                };
                scope.keepClaim = function (folderID, path) {
                    desuqHome.keepClaim(folderID, path).then(function (msg) {
                        scope.claimError[folderID] = msg;
                        if (!msg) {
                            desuqHome.refresh();
                        }
                    });
                };
                scope.releaseClaim = function (folderID, path) {
                    desuqHome.releaseClaim(folderID, path).then(function (msg) {
                        scope.claimError[folderID] = msg;
                        if (!msg) {
                            // Redraw now rather than at the next poll, so the
                            // row goes when the button is pressed.
                            desuqHome.refresh();
                            // Done with a file is the moment somebody knows
                            // what they changed in it. Offered only if they
                            // did change it and have not said why yet.
                            scope.openNote(folderID, path, true);
                        }
                    });
                };

                // "Why I changed this" (lib/api/api_notes.go). One form per
                // card, keyed by folder like everything else here.
                scope.noteWhen = function (iso) { return desuqHome.noteWhen(iso); };
                scope.noteForm = {};

                // path preselects a file; afterDone means the form was offered
                // by Done rather than asked for, and is dropped quietly when
                // there is nothing to say (the file was not changed here, or
                // already has a note).
                scope.openNote = function (folderID, path, afterDone) {
                    var form = {
                        loading: true, path: path || '', text: '', choices: [],
                        error: '', afterDone: !!afterDone, version: null
                    };
                    scope.noteForm[folderID] = form;
                    desuqHome.noteChoices(folderID).then(function (choices) {
                        if (scope.noteForm[folderID] !== form) {
                            return;
                        }
                        form.loading = false;
                        form.choices = choices;
                        var match = choices.filter(function (c) { return c.path === form.path; })[0];
                        if (afterDone && (!match || match.noted)) {
                            delete scope.noteForm[folderID];
                            return;
                        }
                        if (!form.path) {
                            var first = choices.filter(function (c) { return !c.noted; })[0] || choices[0];
                            form.path = first ? first.path : '';
                        } else if (!match) {
                            // From the right-click menu, about a file that is
                            // not among your recent saves: still offered, and
                            // the server says why if it cannot be done.
                            form.choices = [{ path: form.path }].concat(choices);
                        }
                        scope.notePicked(folderID);
                    });
                };
                // A file that already has your note opens with that note in
                // the box: saving is an edit, and an empty box would have
                // replaced it without showing what was there.
                scope.notePicked = function (folderID) {
                    var form = scope.noteForm[folderID];
                    if (!form || form.version) {
                        return;
                    }
                    var rows = (desuqHome.state.notes[folderID] || {}).rows || [];
                    var had = rows.filter(function (n) { return n.mine && n.current && n.path === form.path; })[0];
                    if (had) {
                        form.text = had.text;
                        form.had = true;
                    } else if (form.had) {
                        form.text = '';
                        form.had = false;
                    }
                };
                scope.editNote = function (folderID, n) {
                    scope.noteForm[folderID] = {
                        loading: false, path: n.path, text: n.text, choices: [{ path: n.path }],
                        error: '', afterDone: false, version: { modified: n.modified, size: n.size }
                    };
                };
                scope.closeNote = function (folderID) {
                    delete scope.noteForm[folderID];
                };
                scope.submitNote = function (folderID) {
                    var form = scope.noteForm[folderID];
                    if (!form || !form.path || !String(form.text || '').trim()) {
                        return;
                    }
                    form.busy = true;
                    desuqHome.saveNote(folderID, form.path, form.text, form.version).then(function (msg) {
                        form.busy = false;
                        form.error = msg;
                        if (!msg) {
                            delete scope.noteForm[folderID];
                            desuqHome.refresh();
                        }
                    });
                };
                scope.removeNote = function (folderID, n) {
                    desuqHome.saveNote(folderID, n.path, '', { modified: n.modified, size: n.size }).then(function (msg) {
                        scope.claimError[folderID] = msg;
                        if (!msg) {
                            desuqHome.refresh();
                        }
                    });
                };

                // The .blend right-click menu's "Say why I changed this" opens
                // the GUI at ?desuq-note=<folder>&file=<path> (custom/tray).
                (function openNoteFromURL() {
                    var q = {};
                    String($window.location.search || '').replace(/^\?/, '').split('&').forEach(function (kv) {
                        var i = kv.indexOf('=');
                        if (i > 0) {
                            q[decodeURIComponent(kv.slice(0, i))] = decodeURIComponent(kv.slice(i + 1).replace(/\+/g, ' '));
                        }
                    });
                    if (!q['desuq-note']) {
                        return;
                    }
                    scope.openNote(q['desuq-note'], q.file || '', false);
                    if ($window.history && $window.history.replaceState) {
                        $window.history.replaceState(null, '', $window.location.pathname);
                    }
                })();

                // The picker says so when it rewrites a folder's ignores; the
                // probe is throttled to once a minute otherwise, and waiting
                // that long to notice your own click is the wrong answer.
                // Resolving a conflict happens on the history screen, which
                // has no way back into this service. The count here is on a
                // slow probe, so without this the row would go on claiming a
                // decision that has already been made for up to a minute.
                scope.$on('desuq:conflictsChanged', function () {
                    desuqHome.markConflictsStale();
                    desuqHome.refresh();
                });

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
