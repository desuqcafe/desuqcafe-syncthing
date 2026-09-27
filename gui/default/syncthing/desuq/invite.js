// Added by the desuqcafe fork. Lives in its own directory, which upstream does
// not have, so it can never conflict on a merge.
//
// Inviting somebody, as one thing.
//
// Upstream spreads it over three places and says nothing about the order:
// Add Device (a modal with six tabs, one of which wants a pasted ID),
// then each folder's Sharing tab, then -- in this fork -- a verification card
// that lived in the device's settings. The person on the other end had it
// worse: a yellow "New Device" banner above everything, whose Add button opened
// the same six-tab modal.
//
// Now that nothing syncs until a device is verified
// (lib/model/desuq_verified.go), the order matters as well as the effort:
// verification is best done while both people are on the call that carried
// the code, and a flow that asks for it on a different screen, later, is one
// where it gets done later or not at all.
//
// So this is three steps in the order they actually happen:
//
//   1. who     -- paste their code, or show them yours. Opened from a request
//                 ("Kai wants to connect") the code is already filled in.
//   2. verify  -- the card, with them still on the line.
//   3. share   -- which folders. Ticked here, offered to them on save.
//
// Opened for somebody already added, it starts at step 2: that is the Verify
// button on the main screen.
//
// Saving without verifying is allowed and says what it means. It is not a way
// around the requirement -- the server refuses the connection either way --
// but "we are not on a call right now" is a real situation, and refusing to
// save would only lose what was typed.

angular.module('syncthing.core')

    .factory('desuqInvite', function ($http, $q, $timeout, desuqVerification, desuqWizard) {
        'use strict';

        var STEPS = ['who', 'verify', 'share', 'done'];
        var POLL_MS = 2000;

        var st = {
            open: false,
            step: 0,
            // 'new': somebody not in the config yet. 'existing': opened from
            // their card, to verify or to share more with them.
            mode: 'new',
            // Which half of step 1: 'paste' (I have their code) or 'show'
            // (they need mine).
            whoTab: 'paste',
            myID: '',
            idDraft: '',
            nameDraft: '',
            idError: '',
            checking: false,
            // The device as it will be saved. The card writes
            // desuqVerifiedAt onto it.
            draft: { deviceID: '', desuqVerifiedAt: '' },
            // [{id, label, checked, already}]
            folders: [],
            // Devices asking to connect right now, for the "they need my code"
            // half: the moment they add you, they appear here.
            pending: [],
            saving: false,
            error: '',
            copied: false,
            // What happened, for the last step.
            result: null
        };

        var poller = null;

        function canonical(id) {
            return String(id || '').toUpperCase().replace(/[^A-Z0-9]/g, '');
        }

        function loadConfig() {
            return $q.all([
                $http.get(urlbase + '/system/status').then(function (r) {
                    st.myID = (r.data && r.data.myID) || st.myID;
                }, angular.noop),
                $http.get(urlbase + '/config').then(function (r) {
                    return r.data || {};
                }, function () { return {}; })
            ]).then(function (res) {
                return res[1];
            });
        }

        function refreshPending() {
            return $http.get(urlbase + '/cluster/pending/devices').then(function (r) {
                var out = [];
                angular.forEach(r.data || {}, function (v, id) {
                    out.push({ deviceID: id, name: (v && v.name) || '' });
                });
                st.pending = out;
            }, angular.noop);
        }

        function startPolling() {
            stopPolling();
            (function tick() {
                poller = $timeout(function () {
                    var job = (STEPS[st.step] === 'who' && st.whoTab === 'show') ? refreshPending() : $q.when();
                    job.finally(function () {
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

        function folderList(cfg, deviceID) {
            return (cfg.folders || []).map(function (f) {
                var already = (f.devices || []).some(function (d) { return d.deviceID === deviceID; });
                return { id: f.id, label: f.label || f.id, checked: already, already: already };
            }).sort(function (a, b) { return a.label.localeCompare(b.label); });
        }

        function reset() {
            st.step = 0;
            st.mode = 'new';
            st.whoTab = 'paste';
            st.idDraft = '';
            st.nameDraft = '';
            st.idError = '';
            st.checking = false;
            st.draft = { deviceID: '', desuqVerifiedAt: '' };
            st.folders = [];
            st.saving = false;
            st.error = '';
            st.result = null;
        }

        var svc = {
            state: st,

            stepName: function () {
                return STEPS[st.step];
            },

            // opts: {} for a blank invite, {tab: 'show'} to start on this
            // computer's code; {deviceID, name} from somebody asking to
            // connect; {deviceID} for somebody already added (their card's
            // Verify), with step: 'share' to go straight to the folders.
            open: function (opts) {
                opts = opts || {};
                reset();
                // The setup guide is also a modal; two at once leaves Bootstrap
                // confused about which one owns the scroll.
                if (desuqWizard.state.open) {
                    desuqWizard.close();
                }
                st.whoTab = opts.tab === 'show' ? 'show' : 'paste';
                return loadConfig().then(function (cfg) {
                    var existing = null;
                    if (opts.deviceID) {
                        existing = (cfg.devices || []).filter(function (d) {
                            return canonical(d.deviceID) === canonical(opts.deviceID);
                        })[0] || null;
                    }
                    if (existing) {
                        st.mode = 'existing';
                        st.draft = angular.copy(existing);
                        st.draft.desuqVerifiedAt = st.draft.desuqVerifiedAt || '';
                        st.nameDraft = existing.name || '';
                        st.idDraft = existing.deviceID;
                        st.step = STEPS.indexOf(opts.step === 'share' ? 'share' : 'verify');
                    } else if (opts.deviceID) {
                        st.idDraft = opts.deviceID;
                        st.nameDraft = opts.name || '';
                    }
                    st.folders = folderList(cfg, st.draft.deviceID || opts.deviceID || '');
                    st.open = true;
                    startPolling();
                    refreshPending();
                    return true;
                });
            },

            close: function () {
                st.open = false;
                stopPolling();
            },

            back: function () {
                if (st.step > 0 && !(st.mode === 'existing' && STEPS[st.step] === 'verify')) {
                    st.step -= 1;
                }
            },

            // Step 1 -> 2. Asks Syncthing whether the code is a code, rather
            // than guessing from its length: the check digits are Syncthing's
            // to judge, and it accepts both the 52- and 56-character forms.
            checkID: function () {
                var raw = (st.idDraft || '').trim();
                st.idError = '';
                if (!raw) {
                    st.idError = 'Paste the code they sent you first.';
                    return $q.when(false);
                }
                st.checking = true;
                return $http.get(urlbase + '/svc/deviceid', { params: { id: raw } }).then(function (r) {
                    var d = r.data || {};
                    if (d.error || !d.id) {
                        st.idError = 'That is not a device code. Check it was copied whole — it is eight ' +
                            'groups of seven letters and numbers.';
                        return false;
                    }
                    if (d.id === st.myID) {
                        st.idError = 'That is this computer\'s own code. You need the code from their computer.';
                        return false;
                    }
                    return loadConfig().then(function (cfg) {
                        var existing = (cfg.devices || []).filter(function (x) { return x.deviceID === d.id; })[0];
                        if (existing) {
                            // Already added. Carry on as though opened from
                            // their card rather than making a duplicate.
                            st.mode = 'existing';
                            st.draft = angular.copy(existing);
                            st.draft.desuqVerifiedAt = st.draft.desuqVerifiedAt || '';
                            st.nameDraft = st.nameDraft || existing.name || '';
                        } else {
                            st.draft = {
                                deviceID: d.id,
                                desuqVerifiedAt: desuqVerification.provisional(d.id)
                            };
                        }
                        st.idDraft = d.id;
                        st.folders = folderList(cfg, d.id);
                        st.step = STEPS.indexOf('verify');
                        return true;
                    });
                }, function () {
                    st.idError = 'Could not check the code. Is Syncthing still running?';
                    return false;
                }).finally(function () {
                    st.checking = false;
                });
            },

            // From the "they need my code" half: somebody added this computer
            // and is knocking. Take their code from the request.
            usePending: function (p) {
                st.whoTab = 'paste';
                st.idDraft = p.deviceID;
                st.nameDraft = st.nameDraft || p.name || '';
                return svc.checkID();
            },

            toShare: function () {
                st.step = STEPS.indexOf('share');
            },

            name: function () {
                return (st.nameDraft || '').trim() ||
                    (st.draft.deviceID ? st.draft.deviceID.substring(0, 7) : 'them');
            },

            // Step 3's button. Writes the device (new, or renamed), then adds
            // it to every newly ticked folder. Folders already shared are left
            // alone: unsharing is a destructive decision with its own place.
            save: function () {
                var id = st.draft.deviceID;
                if (!id) {
                    return $q.when(false);
                }
                st.saving = true;
                st.error = '';
                var name = (st.nameDraft || '').trim();
                var verifiedAt = st.draft.desuqVerifiedAt || desuqVerification.provisional(id) || '';

                var writeDevice;
                if (st.mode === 'new') {
                    writeDevice = $http.get(urlbase + '/config/defaults/device').then(function (r) {
                        var dev = angular.copy(r.data || {});
                        dev.deviceID = id;
                        dev.name = name;
                        dev.desuqVerifiedAt = verifiedAt;
                        return $http.post(urlbase + '/config/devices', dev);
                    });
                } else {
                    var patch = {};
                    if (name && name !== st.draft.name) {
                        patch.name = name;
                    }
                    writeDevice = Object.keys(patch).length
                        ? $http.patch(urlbase + '/config/devices/' + encodeURIComponent(id), patch)
                        : $q.when();
                }

                var added = [];
                return writeDevice.then(function () {
                    var jobs = st.folders.filter(function (f) { return f.checked && !f.already; }).map(function (f) {
                        var url = urlbase + '/config/folders/' + encodeURIComponent(f.id);
                        return $http.get(url).then(function (r) {
                            var devices = ((r.data && r.data.devices) || []).slice();
                            if (!devices.some(function (d) { return d.deviceID === id; })) {
                                devices.push({ deviceID: id, introducedBy: '', encryptionPassword: '' });
                            }
                            return $http.patch(url, { devices: devices });
                        }).then(function () {
                            added.push(f.label);
                        });
                    });
                    return $q.all(jobs);
                }).then(function () {
                    st.result = {
                        name: name || id.substring(0, 7),
                        verified: !!verifiedAt,
                        added: added,
                        isNew: st.mode === 'new'
                    };
                    st.step = STEPS.indexOf('done');
                    return true;
                }, function (e) {
                    st.error = 'Could not save' + (e && e.data ? ': ' + String(e.data).trim() : '') +
                        (added.length ? '. ' + added.join(', ') + ' did get shared.' : '.');
                    return false;
                }).finally(function () {
                    st.saving = false;
                });
            },

            copy: function (event, text) {
                var ok = desuqWizard.copy(event, text);
                st.copied = ok;
                if (ok) {
                    $timeout(function () { st.copied = false; }, 2000);
                }
                return ok;
            }
        };

        return svc;
    })

    // Published on $rootScope, like desuqWizard and desuqSelective, so any
    // template -- the main screen, the setup guide -- can open it by name.
    .run(function ($rootScope, desuqInvite) {
        $rootScope.desuqInvite = desuqInvite;
    })

    // <desuq-invite></desuq-invite>, once, beside the wizard in index.html.
    //
    // Plain Bootstrap markup rather than upstream's <modal> directive, for the
    // same reason as the wizard: that directive gates the GUI's start-up on
    // every <modal> in the page having compiled.
    .directive('desuqInvite', function (desuqInvite, $timeout) {
        return {
            restrict: 'E',
            templateUrl: 'syncthing/desuq/inviteView.html',
            scope: {},
            link: function (scope, element) {
                var $modal = null;
                scope.st = desuqInvite.state;
                scope.svc = desuqInvite;

                function modal() {
                    if (!$modal && typeof $ === 'function' && $.fn && $.fn.modal) {
                        $modal = $(element[0].querySelector('.modal'));
                        $modal.on('hidden.bs.modal', function () {
                            if (desuqInvite.state.open) {
                                desuqInvite.close();
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
                    var m = modal();
                    if (!m) {
                        return;
                    }
                    if (open) {
                        m.modal({ backdrop: true, keyboard: true });
                    } else {
                        m.modal('hide');
                    }
                });

                // The card calls this when the right one is picked. A beat
                // first, so the confirmation actually lands on screen before
                // the step changes under it.
                scope.verified = function () {
                    $timeout(function () {
                        if (desuqInvite.stepName() === 'verify') {
                            desuqInvite.toShare();
                        }
                    }, 1100);
                };

                scope.$on('$destroy', function () {
                    desuqInvite.close();
                });
            }
        };
    });
