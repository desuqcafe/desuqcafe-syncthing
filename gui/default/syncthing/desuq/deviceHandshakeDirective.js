// Added by the desuqcafe fork. Lives in its own directory, which upstream does
// not have, so it can never conflict on a merge.
//
// THE PROBLEM
//
// Adding a device in Syncthing is mutual -- both sides have to add the other --
// and that is often mistaken for authentication. It is not. It proves the two
// installations agreed on an ID; it says nothing about *whose* ID it is.
//
// In practice a device ID is pasted into a chat window and trusted. Anyone able
// to edit that message can substitute their own ID, and both people will
// happily add the attacker, see "Connected", and never notice. Syncthing's own
// docs are clear that the ID must be exchanged over a trusted channel; nothing
// in the product helps you check that you did.
//
// THE MECHANISM
//
// This is a short authentication string, the same idea ZRTP and Signal use.
// Hash the two device IDs -- sorted, so both machines compute the same digest
// regardless of who is adding whom -- and render four bytes of the result as
// something a human will actually read out loud:
//
//     《 CRIMSON MOONLIGHT SLASH 》Rank VII
//
// Three lists of 256 words plus a rank of 0-255 is 2^32 possibilities. An
// attacker who put their own ID in the middle produces a different pair, so a
// different phrase, and the two of you hear the mismatch.
//
// WHAT MAKES IT WORK, AND WHAT DOES NOT
//
// The strength is entirely in the channel you compare over. Reading the phrase
// to each other on a voice call works because you recognise the voice; sending
// it through the same chat that carried the ID does not, because whoever
// tampered with the ID can tamper with the phrase too. The UI says so plainly,
// because a verification ritual people perform incorrectly is worse than none:
// it manufactures confidence without earning it.

angular.module('syncthing.core')

    // SHA-256, written out rather than taken from window.crypto.subtle.
    //
    // SubtleCrypto is only defined in a secure context. That covers
    // http://127.0.0.1, but the GUI is routinely reached at http://192.168.x.x
    // from another machine on the LAN, where crypto.subtle is undefined and
    // this would silently stop working -- on exactly the setup most likely to
    // need it. Sixty lines is worth not having that failure mode.
    .factory('desuqSha256', function () {
        var K = [
            0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
            0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
            0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
            0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
            0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
            0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
            0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
            0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
        ];

        function rotr(x, n) { return (x >>> n) | (x << (32 - n)); }

        // Returns the digest as an array of 32 byte values.
        return function sha256(str) {
            var H = [
                0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
                0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19
            ];

            // UTF-8 encode. The inputs here are ASCII device IDs, but a
            // hash that quietly disagreed with itself on non-ASCII would be a
            // miserable bug to find, so do it properly.
            var bytes = [];
            for (var i = 0; i < str.length; i++) {
                var c = str.charCodeAt(i);
                if (c < 0x80) {
                    bytes.push(c);
                } else if (c < 0x800) {
                    bytes.push(0xc0 | (c >> 6), 0x80 | (c & 0x3f));
                } else {
                    bytes.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 0x3f), 0x80 | (c & 0x3f));
                }
            }

            var bitLen = bytes.length * 8;
            bytes.push(0x80);
            while (bytes.length % 64 !== 56) {
                bytes.push(0);
            }
            // Length as a 64-bit big-endian count. The high word is always zero
            // for anything this is ever given.
            bytes.push(0, 0, 0, 0,
                (bitLen >>> 24) & 0xff, (bitLen >>> 16) & 0xff, (bitLen >>> 8) & 0xff, bitLen & 0xff);

            var w = new Array(64);
            for (var off = 0; off < bytes.length; off += 64) {
                for (var t = 0; t < 16; t++) {
                    w[t] = (bytes[off + t * 4] << 24) | (bytes[off + t * 4 + 1] << 16) |
                        (bytes[off + t * 4 + 2] << 8) | bytes[off + t * 4 + 3];
                }
                for (t = 16; t < 64; t++) {
                    var s0 = rotr(w[t - 15], 7) ^ rotr(w[t - 15], 18) ^ (w[t - 15] >>> 3);
                    var s1 = rotr(w[t - 2], 17) ^ rotr(w[t - 2], 19) ^ (w[t - 2] >>> 10);
                    w[t] = (w[t - 16] + s0 + w[t - 7] + s1) | 0;
                }

                var a = H[0], b = H[1], c = H[2], d = H[3];
                var e = H[4], f = H[5], g = H[6], h = H[7];

                for (t = 0; t < 64; t++) {
                    var S1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25);
                    var ch = (e & f) ^ (~e & g);
                    var t1 = (h + S1 + ch + K[t] + w[t]) | 0;
                    var S0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22);
                    var maj = (a & b) ^ (a & c) ^ (b & c);
                    var t2 = (S0 + maj) | 0;

                    h = g; g = f; f = e; e = (d + t1) | 0;
                    d = c; c = b; b = a; a = (t1 + t2) | 0;
                }

                H[0] = (H[0] + a) | 0; H[1] = (H[1] + b) | 0;
                H[2] = (H[2] + c) | 0; H[3] = (H[3] + d) | 0;
                H[4] = (H[4] + e) | 0; H[5] = (H[5] + f) | 0;
                H[6] = (H[6] + g) | 0; H[7] = (H[7] + h) | 0;
            }

            var out = [];
            for (i = 0; i < 8; i++) {
                out.push((H[i] >>> 24) & 0xff, (H[i] >>> 16) & 0xff, (H[i] >>> 8) & 0xff, H[i] & 0xff);
            }
            return out;
        };
    })

    .factory('desuqHandshake', ['desuqSha256', 'desuqHandshakeWords',
        function (sha256, words) {

            // Device IDs are shown with dashes and, depending on where they
            // came from, in the 52- or 56-character form. Both sides must
            // agree byte for byte before hashing or they compute different
            // phrases from the same pair.
            function canonical(id) {
                if (!id) {
                    return '';
                }
                return String(id).toUpperCase().replace(/[^A-Z0-9]/g, '');
            }

            function roman(n) {
                var table = [
                    [1000, 'M'], [900, 'CM'], [500, 'D'], [400, 'CD'],
                    [100, 'C'], [90, 'XC'], [50, 'L'], [40, 'XL'],
                    [10, 'X'], [9, 'IX'], [5, 'V'], [4, 'IV'], [1, 'I']
                ];
                var out = '';
                for (var i = 0; i < table.length; i++) {
                    while (n >= table[i][0]) {
                        out += table[i][1];
                        n -= table[i][0];
                    }
                }
                return out;
            }

            return {
                // valid reports whether an ID is long enough to be a real
                // device ID. Deliberately lax -- the point is to avoid
                // computing a phrase for half-typed input, not to validate.
                valid: function (id) {
                    return canonical(id).length >= 52;
                },

                // of returns the phrase for a pair of devices, or null if
                // either ID is not usable yet.
                //
                // Sorting the two IDs before hashing is what makes both
                // machines agree: whoever is adding whom, the input is the
                // same string, so the digest is the same.
                of: function (localID, remoteID) {
                    var a = canonical(localID);
                    var b = canonical(remoteID);
                    if (a.length < 52 || b.length < 52 || a === b) {
                        return null;
                    }

                    var digest = sha256(a < b ? a + ':' + b : b + ':' + a);
                    var rank = digest[3];

                    return {
                        aspect: words.aspect[digest[0]],
                        omen: words.omen[digest[1]],
                        strike: words.strike[digest[2]],
                        // Displayed 1-256 rather than 0-255: "Rank 0" reads as
                        // a bug report, and the range is what carries the
                        // entropy, not where it starts.
                        rank: rank + 1,
                        rankRoman: roman(rank + 1),
                        phrase: words.aspect[digest[0]] + ' ' +
                            words.omen[digest[1]] + ' ' +
                            words.strike[digest[2]]
                    };
                }
            };
        }])

    // <device-handshake local-id="myID" remote-id="currentDevice.deviceID">
    //
    // Both IDs are watched, so the card follows the Device ID field as it is
    // pasted in rather than needing the modal reopened.
    .directive('deviceHandshake', ['desuqHandshake', '$window',
        function (handshake, $window) {
            // Confirmations live in localStorage rather than the Syncthing
            // config. Writing to the config would need a REST call and would
            // sync a claim about identity between machines, which is precisely
            // the thing that cannot be trusted over the wire. It is a note to
            // self, and it belongs on this machine only.
            var STORE = 'desuq.handshake.confirmed';

            function load() {
                try {
                    return JSON.parse($window.localStorage.getItem(STORE)) || {};
                } catch (e) {
                    return {};
                }
            }

            function save(map) {
                try {
                    $window.localStorage.setItem(STORE, JSON.stringify(map));
                } catch (e) {
                    // Private browsing, storage disabled, quota. The card still
                    // works; it just will not remember.
                }
            }

            return {
                restrict: 'E',
                scope: {
                    localId: '=',
                    remoteId: '=',
                    compact: '=?'
                },
                // Deliberately not using the translate directive:
                // angular-translate renders {%placeholders%} literally for any
                // string it has no entry for, and none of these will ever be in
                // upstream's translation files.
                template: [
                    '<div class="desuq-handshake" ng-if="sas" ng-class="{\'desuq-handshake-confirmed\': confirmed, \'desuq-handshake-compact\': compact}">',
                    '  <div class="desuq-handshake-card">',
                    '    <div class="desuq-handshake-rank">',
                    '      <span class="desuq-handshake-rank-numeral">{{sas.rankRoman}}</span>',
                    '      <span class="desuq-handshake-rank-arabic">{{sas.rank}}</span>',
                    '    </div>',
                    '    <div class="desuq-handshake-body">',
                    '      <div class="desuq-handshake-label">',
                    '        <span ng-if="!confirmed">Say this out loud &mdash; it must match on both screens</span>',
                    '        <span ng-if="confirmed">Confirmed on this computer {{confirmedAt}}</span>',
                    '      </div>',
                    '      <div class="desuq-handshake-phrase">',
                    '        <span class="desuq-handshake-bracket">&#12298;</span>',
                    '        <span class="desuq-handshake-word">{{sas.aspect}}</span>',
                    '        <span class="desuq-handshake-word">{{sas.omen}}</span>',
                    '        <span class="desuq-handshake-word">{{sas.strike}}</span>',
                    '        <span class="desuq-handshake-bracket">&#12299;</span>',
                    '      </div>',
                    '      <div class="desuq-handshake-rank-line">Rank {{sas.rankRoman}} <span class="desuq-handshake-dim">({{sas.rank}} of 256)</span></div>',
                    '    </div>',
                    '  </div>',
                    '  <div class="desuq-handshake-note" ng-if="!compact">',
                    '    <p>',
                    '      <span class="fa fa-phone"></span>',
                    '      <strong>Call the other person and read it to each other.</strong>',
                    '      If both screens show the same three words and the same rank, the',
                    '      device really belongs to them.',
                    '    </p>',
                    '    <p class="desuq-handshake-warning">',
                    '      <span class="fa fa-exclamation-triangle"></span>',
                    '      Comparing it <em>inside</em> Syncthing, or over the same chat you used to',
                    '      send the device ID, proves nothing &mdash; anyone who could swap the ID',
                    '      could swap this too. Only a channel you already trust, like recognising',
                    '      their voice, makes it mean anything.',
                    '    </p>',
                    '  </div>',
                    '  <button type="button" class="btn btn-sm desuq-handshake-btn" ng-if="!compact && !confirmed" ng-click="confirm()">',
                    '    <span class="fa fa-check"></span> We read it aloud and it matched',
                    '  </button>',
                    '  <button type="button" class="btn btn-sm desuq-handshake-btn desuq-handshake-btn-undo" ng-if="!compact && confirmed" ng-click="unconfirm()">',
                    '    <span class="fa fa-undo"></span> Mark as not verified',
                    '  </button>',
                    '</div>'
                ].join('\n'),
                link: function (scope) {
                    function key() {
                        return String(scope.remoteId || '').toUpperCase().replace(/[^A-Z0-9]/g, '');
                    }

                    function refresh() {
                        scope.sas = handshake.of(scope.localId, scope.remoteId);
                        if (!scope.sas) {
                            scope.confirmed = false;
                            return;
                        }
                        var record = load()[key()];
                        // A stored confirmation is only good for the phrase it
                        // was made against. If either device ID changed, the
                        // phrase changes and the old confirmation is void --
                        // otherwise re-pasting a different ID would inherit a
                        // tick it never earned.
                        scope.confirmed = !!record && record.phrase === scope.sas.phrase &&
                            record.rank === scope.sas.rank;
                        scope.confirmedAt = scope.confirmed ? record.at : '';
                    }

                    scope.confirm = function () {
                        if (!scope.sas) {
                            return;
                        }
                        var map = load();
                        map[key()] = {
                            phrase: scope.sas.phrase,
                            rank: scope.sas.rank,
                            at: new Date().toLocaleDateString()
                        };
                        save(map);
                        refresh();
                    };

                    scope.unconfirm = function () {
                        var map = load();
                        delete map[key()];
                        save(map);
                        refresh();
                    };

                    scope.$watch('remoteId', refresh);
                    scope.$watch('localId', refresh);
                }
            };
        }]);
