// Added by the desuqcafe fork. Lives in its own directory, which upstream does
// not have, so it can never conflict on a merge.
//
// The first ten minutes.
//
// Everything else this fork adds improves a screen somebody has already found.
// This one exists because of the screen nobody can act on at all: a freshly
// installed Syncthing with no folders and no devices. Upstream renders an
// empty folder list, an "Add Folder" button, and a device panel showing only
// this machine. There is no next action anywhere on it, and the one thing the
// person actually needs -- their device ID, to send to whoever is sharing
// files with them -- is behind a menu called "Actions", under an entry called
// "Show ID", in a modal that also offers to share it by SMS.
//
// For the two non-technical modellers this fork is built for, that screen is
// where the install ends and the phone call starts.
//
// Four steps, in the order they actually happen:
//
//   1. name    -- what this machine is called, because "Alex" and
//                 "DESKTOP-A1B2C3" both tell the other end nothing
//   2. code    -- the device ID, large, with Copy and the QR upstream already
//                 serves at /qr/, and a live indicator that notices the moment
//                 somebody adds you
//   3. verify  -- the handshake card from deviceHandshakeDirective.js, at the
//                 one moment it is actually useful: while the other person is
//                 still on the call
//   4. sync    -- what a folder offer looks like when it lands, and the button
//                 that hands it to the picker
//
// **Every step is resumable, and none of them traps you.** Step 2 cannot
// complete until somebody at the other end adds this device, and that will
// routinely be tomorrow rather than in the next thirty seconds -- so the
// wizard closes on the Escape key, on the backdrop, and on an explicit
// "Finish this later", remembers which step it was on, and reopens there from
// Actions -> Setup guide. A wizard that demands a stranger's attention before
// it will let go of the screen is worse than no wizard: the person closes the
// browser tab and never comes back.
//
// It reads its own state over REST rather than off syncthingController's
// scope. Two reasons: the merge cost of this whole feature stays at "some
// additive lines in index.html", and a REST-driven directive can be rendered
// against a live instance by custom/scripts/test-wizard-render.js without
// standing up the entire controller.

angular.module('syncthing.core')

    .factory('desuqWizard', function ($http, $q, $timeout, $window) {
        'use strict';

        // Where "how far did I get" lives. localStorage rather than the
        // config for the same reason the handshake confirmations do: it is a
        // note to self about this browser on this machine, and writing it to
        // the config would sync one person's onboarding progress to everybody
        // else's device list.
        var STORE = 'desuq.wizard';

        // The handshake directive's own store, read (never written) here so
        // step 3 can tick itself off when a confirmation already exists.
        var HANDSHAKE_STORE = 'desuq.handshake.confirmed';

        var STEPS = ['name', 'code', 'verify', 'sync'];

        // While the wizard is on screen it re-reads its inputs on this
        // interval. Everything it polls is local and small -- the config, the
        // pending lists, the connection map -- and the whole point of step 2
        // is noticing an event that arrives while the person is looking at it.
        var POLL_MS = 2000;

        var st = {
            open: false,
            step: 0,
            steps: STEPS,
            // Set once the last step is passed, or by "Finish this later".
            // Only `completed` suppresses the automatic first-run open.
            completed: false,
            error: null,
            saving: false,
            copied: false,

            myID: '',
            myName: '',
            nameDraft: '',

            // [{deviceID, name, connected, clientVersion, verified}]
            peers: [],
            // Devices that have tried to connect and have not been added.
            pending: [],
            // [{folderID, label, deviceID, deviceName}]
            offers: [],
            folderCount: 0
        };

        var poller = null;

        function load() {
            try {
                return JSON.parse($window.localStorage.getItem(STORE)) || {};
            } catch (e) {
                return {};
            }
        }

        function save(rec) {
            try {
                $window.localStorage.setItem(STORE, JSON.stringify(rec));
            } catch (e) {
                // Private browsing, storage disabled, quota. The wizard still
                // works; it just stops remembering where it got to, which
                // makes it annoying rather than broken.
            }
        }

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

        // Read everything the four steps need. Deliberately tolerant: a
        // request that fails leaves the previous answer in place rather than
        // blanking the screen, because the most likely cause is Syncthing
        // restarting under us and the next tick will succeed.
        function refresh() {
            var jobs = [
                $http.get(urlbase + '/system/status').then(function (r) {
                    st.myID = (r.data && r.data.myID) || st.myID;
                }, angular.noop),

                $http.get(urlbase + '/config').then(function (r) {
                    var cfg = r.data || {};
                    var devices = cfg.devices || [];
                    var confirmed = confirmations();

                    st.folderCount = (cfg.folders || []).length;

                    var peers = [];
                    devices.forEach(function (d) {
                        if (!d.deviceID || d.deviceID === st.myID) {
                            if (d.deviceID === st.myID) {
                                st.myName = d.name || '';
                                if (!st.nameDraft) {
                                    st.nameDraft = st.myName;
                                }
                            }
                            return;
                        }
                        peers.push({
                            deviceID: d.deviceID,
                            name: d.name || d.deviceID.substring(0, 7),
                            verified: !!confirmed[normalise(d.deviceID)],
                            connected: false,
                            clientVersion: ''
                        });
                    });
                    st.peers = peers;
                }, angular.noop),

                $http.get(urlbase + '/cluster/pending/devices').then(function (r) {
                    var out = [];
                    angular.forEach(r.data || {}, function (v, id) {
                        out.push({ deviceID: id, name: (v && v.name) || '' });
                    });
                    st.pending = out;
                }, angular.noop),

                $http.get(urlbase + '/cluster/pending/folders').then(function (r) {
                    var out = [];
                    angular.forEach(r.data || {}, function (v, folderID) {
                        angular.forEach((v && v.offeredBy) || {}, function (offer, deviceID) {
                            out.push({
                                folderID: folderID,
                                label: (offer && offer.label) || folderID,
                                deviceID: deviceID
                            });
                        });
                    });
                    st.offers = out;
                }, angular.noop),

                $http.get(urlbase + '/system/connections').then(function (r) {
                    var conns = (r.data && r.data.connections) || {};
                    st.peers.forEach(function (p) {
                        var c = conns[p.deviceID];
                        if (c) {
                            p.connected = !!c.connected;
                            p.clientVersion = c.clientVersion || '';
                        }
                    });
                }, angular.noop)
            ];

            return $q.all(jobs).then(function () {
                // Names for the folder offers, now that the device list is in.
                st.offers.forEach(function (o) {
                    var peer = st.peers.filter(function (p) {
                        return p.deviceID === o.deviceID;
                    })[0];
                    o.deviceName = peer ? peer.name : o.deviceID.substring(0, 7);
                });
                return st;
            });
        }

        function startPolling() {
            stopPolling();
            (function tick() {
                poller = $timeout(function () {
                    refresh().finally(function () {
                        if (st.open) {
                            tick();
                        }
                    });
                }, POLL_MS);
            })();
        }

        function stopPolling() {
            if (poller) {
                $timeout.cancel(poller);
                poller = null;
            }
        }

        // Has this step been satisfied by the state of the world? Used to draw
        // the tick beside each step, and to decide whether Next is the primary
        // action or the muted one. Never used to *block* Next -- see the
        // header: a step that cannot be completed today must still be
        // walk-past-able.
        function isDone(name) {
            switch (name) {
                case 'name':
                    return !!st.myName;
                case 'code':
                    return st.peers.length > 0 || st.pending.length > 0;
                case 'verify':
                    return st.peers.some(function (p) { return p.verified; });
                case 'sync':
                    return st.folderCount > 0;
                default:
                    return false;
            }
        }

        var svc = {
            state: st,

            stepName: function () {
                return STEPS[st.step];
            },

            isDone: isDone,

            // Everything the wizard exists to walk somebody through has
            // happened, so there is nothing left for it to say.
            allDone: function () {
                return STEPS.every(isDone);
            },

            // `force` is a human clicking "Setup guide"; without it this is
            // the automatic first-run open, which has conditions.
            open: function (force) {
                var rec = load();
                return refresh().then(function () {
                    if (!force) {
                        // Finished once: never ambush them again.
                        if (rec.completed) {
                            return false;
                        }
                        // Somebody who already has a peer or a folder found
                        // their own way here, and a wizard over the top of a
                        // working setup is an interruption, not help.
                        if (st.peers.length > 0 || st.folderCount > 0) {
                            return false;
                        }
                    }
                    st.completed = !!rec.completed;
                    // Clamped, because a stored step outlives a change to the
                    // number of steps.
                    st.step = Math.min(rec.step || 0, STEPS.length - 1);
                    st.nameDraft = st.myName;
                    st.error = null;
                    st.open = true;
                    startPolling();
                    return true;
                });
            },

            next: function () {
                if (st.step < STEPS.length - 1) {
                    st.step += 1;
                    svc.remember();
                } else {
                    svc.finish();
                }
            },

            back: function () {
                if (st.step > 0) {
                    st.step -= 1;
                    svc.remember();
                }
            },

            goTo: function (i) {
                if (i >= 0 && i < STEPS.length) {
                    st.step = i;
                    svc.remember();
                }
            },

            remember: function () {
                var rec = load();
                rec.step = st.step;
                save(rec);
            },

            // "Finish this later" and the close button are the same thing:
            // remember the step, close, stay reopenable. Neither marks the
            // wizard completed, so the tick list is still waiting where they
            // left it.
            close: function () {
                st.open = false;
                stopPolling();
                svc.remember();
            },

            // The end of the last step. This is the only thing that stops the
            // wizard opening itself again.
            finish: function () {
                var rec = load();
                rec.completed = true;
                rec.step = 0;
                save(rec);
                st.completed = true;
                st.open = false;
                stopPolling();
            },

            saveName: function () {
                var name = (st.nameDraft || '').trim();
                if (!name || !st.myID || name === st.myName) {
                    svc.next();
                    return $q.when(null);
                }
                st.saving = true;
                st.error = null;
                return $http.patch(
                    urlbase + '/config/devices/' + encodeURIComponent(st.myID),
                    { name: name })
                    .then(function () {
                        st.myName = name;
                        svc.next();
                    }, function (e) {
                        // Worth surfacing rather than swallowing: the name is
                        // the one thing on this screen the other two people
                        // will see, and silently keeping the old one would be
                        // confusing later.
                        st.error = 'Could not save the name' +
                            (e && e.data ? ': ' + e.data : '') + '.';
                    })
                    .finally(function () {
                        st.saving = false;
                    });
            },

            // Modelled on upstream's copyToClipboard. navigator.clipboard is
            // undefined outside a secure context, and the GUI is routinely
            // opened at http://192.168.x.x from the machine next to it -- the
            // same trap that made the handshake carry its own SHA-256. So the
            // execCommand path is not a legacy fallback here, it is the one
            // that runs for half the users.
            copy: function (event, text) {
                var ok = false;
                try {
                    if ($window.navigator.clipboard && $window.navigator.clipboard.writeText) {
                        $window.navigator.clipboard.writeText(text);
                        ok = true;
                    } else if ($window.document.queryCommandSupported) {
                        var host = (event && event.currentTarget) || $window.document.body;
                        var ta = $window.document.createElement('textarea');
                        ta.value = text;
                        // Inside a Bootstrap modal an off-screen textarea does
                        // not receive the selection; opacity and position are
                        // what upstream landed on too.
                        ta.style.position = 'absolute';
                        ta.style.opacity = '0';
                        host.appendChild(ta);
                        ta.select();
                        ok = $window.document.execCommand('copy');
                        host.removeChild(ta);
                    }
                } catch (e) {
                    ok = false;
                }
                st.copied = ok;
                if (ok) {
                    $timeout(function () { st.copied = false; }, 2000);
                }
                return ok;
            },

            refresh: refresh
        };

        return svc;
    })

    // Published on $rootScope so the Actions menu entry in index.html can
    // reach it with one additive line and no change to syncthingController's
    // dependency list. Same pattern as desuqSelective.
    .run(function ($rootScope, desuqWizard) {
        $rootScope.desuqWizard = desuqWizard;
    })

    // <desuq-first-run-wizard my-id="myID" pending-folders="pendingFolders"
    //                         on-accept-folder="addFolderAndShare(folderID, pendingFolder, device)">
    //
    // The bindings are all optional. `onAcceptFolder` is the one that matters:
    // it lets step 4 hand a folder offer to upstream's own accept flow -- and
    // therefore to the fork's picker, which hangs off it -- rather than this
    // file re-implementing addFolderInit's path and type defaults and drifting
    // out of step with them. Without the binding step 4 still explains what
    // will happen; it just cannot press the button for you.
    .directive('desuqFirstRunWizard', function (desuqWizard, $timeout, $window) {
        return {
            restrict: 'E',
            templateUrl: 'syncthing/desuq/firstRunWizardView.html',
            scope: {
                myId: '=?',
                pendingFolders: '=?',
                onAcceptFolder: '&?'
            },
            link: function (scope) {
                var $modal = null;
                scope.st = desuqWizard.state;
                scope.svc = desuqWizard;

                function element() {
                    if (!$modal) {
                        $modal = $('#desuqWizard');
                        $modal.on('hidden.bs.modal', function () {
                            if (desuqWizard.state.open) {
                                desuqWizard.close();
                                scope.$applyAsync();
                            }
                        });
                    }
                    return $modal;
                }

                scope.$watch('st.open', function (open, was) {
                    if (open === was) {
                        return;
                    }
                    if (open) {
                        // Not `backdrop: 'static'`: unlike the picker, which is
                        // mid-way through creating a folder and must not be
                        // abandoned, everything here is safe to walk away from.
                        // Clicking outside closing it is the correct behaviour.
                        element().modal({ backdrop: true, keyboard: true });
                    } else if ($modal) {
                        $modal.modal('hide');
                    }
                });

                scope.acceptOffer = function (offer) {
                    var pending = scope.pendingFolders && scope.pendingFolders[offer.folderID];
                    if (!scope.onAcceptFolder || !pending) {
                        return;
                    }
                    // Upstream's accept opens the folder editor, which is
                    // another modal. Let this one finish hiding first --
                    // Bootstrap 3 takes "modal-open" off <body> when the first
                    // of two overlapping modals hides, and the second is left
                    // unscrollable. The picker learned this the hard way.
                    desuqWizard.close();
                    $timeout(function () {
                        scope.onAcceptFolder({
                            folderID: offer.folderID,
                            pendingFolder: pending,
                            device: offer.deviceID
                        });
                    }, 300);
                };

                // The automatic first-run open. Waits for myID so the code
                // step has something to show the moment it is reached, and
                // runs once.
                var stop = scope.$watch('myId', function (id) {
                    if (!id) {
                        return;
                    }
                    stop();
                    // A tick, so the rest of the GUI has finished its own
                    // start-up before a modal lands on top of it.
                    $timeout(function () {
                        desuqWizard.open(false);
                    }, 800);
                });

                scope.$on('$destroy', function () {
                    if (scope.st.open) {
                        desuqWizard.close();
                    }
                });

                // Nothing here should stop the page unloading.
                angular.element($window).on('beforeunload', function () {
                    desuqWizard.close();
                });
            }
        };
    });
