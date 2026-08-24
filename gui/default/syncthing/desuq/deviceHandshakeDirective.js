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
//     《 CRIMSON TALISMAN NOCTURNE 》Rank CLXXVI
//
// Three lists of 256 words plus a rank of 0-255 is 2^32 possibilities. An
// attacker who put their own ID in the middle produces a different pair, so a
// different phrase, and the two of you notice the mismatch.
//
// WHY IT IS A CARD, AND WHY YOU PICK ONE OF THREE
//
// The failure mode of every "compare these codes" dialogue ever shipped is that
// people click yes without reading. A checkbox saying "it matched" gets ticked
// by reflex.
//
// So confirming deals three cards -- the real one and two decoys -- and asks
// which one the other person is describing. Getting it right requires having
// actually listened. It adds no entropy, and is not meant to: it is an
// attention check on a step whose whole value is that a human really looked.
//
// The card carries the same four bytes three ways -- words, rank, and a sigil
// drawn from them -- so a mismatch is visible as well as audible. Someone who
// would skim "CLXXVI" will still notice a nine-pointed star where theirs has
// six.
//
// WHAT MAKES IT WORK, AND WHAT DOES NOT
//
// The strength is entirely in the channel you compare over. It has to be a
// *different* channel from the one that carried the device ID: whoever could
// tamper with that one can tamper with the phrase too. A call where you
// recognise each other's voice is the strongest version, but the rule is
// "not the same channel", not "must be voice". The UI says so, because a
// verification ritual people perform incorrectly is worse than none -- it
// manufactures confidence without earning it.

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

            // UTF-8 encode. The inputs here are ASCII device IDs, but a hash
            // that quietly disagreed with itself on non-ASCII would be a
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

                var a = H[0], b = H[1], c2 = H[2], d = H[3];
                var e = H[4], f = H[5], g = H[6], h = H[7];

                for (t = 0; t < 64; t++) {
                    var S1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25);
                    var ch = (e & f) ^ (~e & g);
                    var t1 = (h + S1 + ch + K[t] + w[t]) | 0;
                    var S0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22);
                    var maj = (a & b) ^ (a & c2) ^ (b & c2);
                    var t2 = (S0 + maj) | 0;

                    h = g; g = f; f = e; e = (d + t1) | 0;
                    d = c2; c2 = b; b = a; a = (t1 + t2) | 0;
                }

                H[0] = (H[0] + a) | 0; H[1] = (H[1] + b) | 0;
                H[2] = (H[2] + c2) | 0; H[3] = (H[3] + d) | 0;
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

            // Rarity is a redundant encoding of the top of the rank, and that
            // is the point: "mine's gold" is a check someone will actually
            // perform before they have read a single character.
            function tierFor(rank) {
                if (rank >= 249) { return { key: 'legendary', name: 'LEGENDARY' }; }
                if (rank >= 225) { return { key: 'epic', name: 'EPIC' }; }
                if (rank >= 161) { return { key: 'rare', name: 'RARE' }; }
                return { key: 'common', name: 'COMMON' };
            }

            // cardOf builds the whole visible identity from four byte values.
            // Everything on screen -- words, rank, rarity, sigil -- comes from
            // these and nothing else, so two cards that look alike really are
            // the same four bytes.
            function cardOf(ai, bi, ci, ri) {
                var rank = ri + 1;
                var tier = tierFor(rank);
                return {
                    ai: ai, bi: bi, ci: ci, ri: ri,
                    aspect: words.aspect[ai],
                    omen: words.omen[bi],
                    strike: words.strike[ci],
                    rank: rank,
                    rankRoman: roman(rank),
                    tier: tier.key,
                    tierName: tier.name,
                    phrase: words.aspect[ai] + ' ' + words.omen[bi] + ' ' + words.strike[ci]
                };
            }

            return {
                // valid reports whether an ID is long enough to be a real
                // device ID. Deliberately lax -- the point is to avoid
                // computing a phrase for half-typed input, not to validate.
                valid: function (id) {
                    return canonical(id).length >= 52;
                },

                cardOf: cardOf,

                // of returns the card for a pair of devices, or null if either
                // ID is not usable yet.
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
                    var card = cardOf(digest[0], digest[1], digest[2], digest[3]);
                    card.digest = digest;
                    return card;
                },

                // decoysFor returns two cards that are not the real one, for
                // the pick-one-of-three check.
                //
                // Derived from the digest rather than Math.random so the same
                // pair always deals the same three cards: a spread that
                // reshuffled on every redraw would look like the phrase itself
                // was unstable, which is the one impression this must not give.
                //
                // The offset is never zero and the ranks are forced apart, so
                // a decoy can never coincide with the real card in any of the
                // four positions.
                decoysFor: function (card) {
                    var digest = card.digest || [];
                    var out = [];
                    var usedRanks = [card.rank];

                    for (var probe = 0; probe < 512 && out.length < 2; probe++) {
                        var seed = digest.length ? digest[4 + (probe % 8)] : probe * 17;
                        var offset = 1 + ((seed + probe * 37) % 255);
                        var d = cardOf(
                            (card.ai + offset) % 256,
                            (card.bi + offset * 3) % 256,
                            (card.ci + offset * 7) % 256,
                            (card.ri + offset) % 256
                        );
                        if (usedRanks.indexOf(d.rank) === -1) {
                            usedRanks.push(d.rank);
                            out.push(d);
                        }
                    }
                    return out;
                }
            };
        }])

    // The sigil: a small procedural emblem drawn from the same four bytes as
    // the words.
    //
    // Built with DOM calls rather than an interpolated SVG string, the way
    // upstream's identiconDirective does, so nothing has to be marked as
    // trusted HTML and $sanitize is not involved.
    .directive('desuqSigil', function () {
        var NS = 'http://www.w3.org/2000/svg';

        function el(name, attrs) {
            var node = document.createElementNS(NS, name);
            for (var k in attrs) {
                if (Object.prototype.hasOwnProperty.call(attrs, k)) {
                    node.setAttribute(k, attrs[k]);
                }
            }
            return node;
        }

        function polygonPoints(cx, cy, r, sides, rotation) {
            var pts = [];
            for (var i = 0; i < sides; i++) {
                var a = rotation + (i * 2 * Math.PI / sides);
                pts.push((cx + r * Math.cos(a)).toFixed(2) + ',' + (cy + r * Math.sin(a)).toFixed(2));
            }
            return pts.join(' ');
        }

        function draw(card) {
            var svg = el('svg', { viewBox: '0 0 100 100', class: 'desuq-sigil-svg' });
            if (!card) {
                return svg;
            }

            // Each byte drives one visible property, so any single-byte
            // difference between two cards changes the drawing.
            var points = 5 + (card.ai % 8);        // 5..12 star points
            var sides = 3 + (card.bi % 6);         // 3..8 inner polygon
            var orbits = 3 + (card.ci % 10);       // 3..12 orbiting marks
            var spin = (card.ri / 256) * Math.PI * 2;

            var g = el('g', { class: 'desuq-sigil-rot' });

            g.appendChild(el('circle', { cx: 50, cy: 50, r: 46, class: 'desuq-sigil-ring' }));
            g.appendChild(el('circle', { cx: 50, cy: 50, r: 38, class: 'desuq-sigil-ring-thin' }));

            // The star: every point joined to the one `skip` places along,
            // which is what gives the classic pentagram-ish look.
            var skip = 2 + (card.ri % Math.max(1, Math.floor(points / 2)));
            var star = '';
            for (var i = 0; i < points; i++) {
                var a1 = spin + (i * 2 * Math.PI / points);
                var a2 = spin + (((i + skip) % points) * 2 * Math.PI / points);
                star += 'M' + (50 + 34 * Math.cos(a1)).toFixed(2) + ',' + (50 + 34 * Math.sin(a1)).toFixed(2) +
                    'L' + (50 + 34 * Math.cos(a2)).toFixed(2) + ',' + (50 + 34 * Math.sin(a2)).toFixed(2);
            }
            g.appendChild(el('path', { d: star, class: 'desuq-sigil-star' }));

            g.appendChild(el('polygon', {
                points: polygonPoints(50, 50, 20, sides, -Math.PI / 2 + spin),
                class: 'desuq-sigil-core'
            }));

            for (i = 0; i < orbits; i++) {
                var a = spin + (i * 2 * Math.PI / orbits);
                g.appendChild(el('circle', {
                    cx: (50 + 43 * Math.cos(a)).toFixed(2),
                    cy: (50 + 43 * Math.sin(a)).toFixed(2),
                    r: 1.9,
                    class: 'desuq-sigil-mark'
                }));
            }

            svg.appendChild(g);
            return svg;
        }

        return {
            restrict: 'E',
            scope: { card: '=' },
            link: function (scope, element) {
                scope.$watch('card', function (card) {
                    element.empty();
                    element.append(draw(card));
                });
            }
        };
    })

    // One card face. Used for the hero card and for each of the three dealt.
    .directive('desuqCard', function () {
        return {
            restrict: 'E',
            scope: { card: '=', size: '@' },
            template: [
                '<div class="desuq-card desuq-card-{{card.tier}}" ng-class="\'desuq-card-\' + (size || \'md\')">',
                '  <div class="desuq-card-foil"></div>',
                '  <div class="desuq-card-inner">',
                '    <div class="desuq-card-top">',
                '      <span class="desuq-card-tier">{{card.tierName}}</span>',
                '      <span class="desuq-card-rank">{{card.rankRoman}}</span>',
                '    </div>',
                '    <div class="desuq-card-art">',
                '      <desuq-sigil card="card"></desuq-sigil>',
                '    </div>',
                '    <div class="desuq-card-name">',
                '      <span class="desuq-card-word">{{card.aspect}}</span>',
                '      <span class="desuq-card-word">{{card.omen}}</span>',
                '      <span class="desuq-card-word desuq-card-word-strike">{{card.strike}}</span>',
                '    </div>',
                '    <div class="desuq-card-foot">Rank {{card.rankRoman}} &middot; {{card.rank}}/256</div>',
                '  </div>',
                '</div>'
            ].join('\n')
        };
    })

    // <device-handshake local-id="myID" remote-id="currentDevice.deviceID">
    //
    // Both IDs are watched, so the card follows the Device ID field as it is
    // pasted in rather than needing the modal reopened.
    .directive('deviceHandshake', ['desuqHandshake', '$window', '$timeout',
        function (handshake, $window, $timeout) {
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
                    '<div class="desuq-handshake" ng-if="sas"',
                    '     ng-class="{\'desuq-handshake-confirmed\': confirmed, \'desuq-handshake-compact\': compact}">',

                    // --- hero ---
                    //
                    // Hidden while the check is running, and that is the whole
                    // point of the check. The hero carries the real card, the
                    // phrase spelled out in words, and the rank in two
                    // notations; with it on screen, picking the right card out
                    // of three is a matching exercise anyone can pass without
                    // having listened to a single word the other person said.
                    // The check is meant to cost the attention it claims to
                    // prove.
                    '  <div class="desuq-handshake-hero" ng-if="!challenge">',
                    '    <desuq-card card="sas" size="{{compact ? \'sm\' : \'md\'}}"></desuq-card>',
                    '    <div class="desuq-handshake-side" ng-if="!compact">',
                    '      <div class="desuq-handshake-status" ng-if="confirmed">',
                    '        <span class="fa fa-check-circle"></span> Verified on this computer {{confirmedAt}}',
                    '      </div>',
                    '      <div class="desuq-handshake-status desuq-handshake-status-pending" ng-if="!confirmed">',
                    '        <span class="fa fa-exclamation-circle"></span> Not verified yet',
                    '      </div>',
                    '      <div class="desuq-handshake-phrase">',
                    '        <span class="desuq-handshake-bracket">&#12298;</span>',
                    '        <span class="desuq-handshake-word">{{sas.aspect}}</span>',
                    '        <span class="desuq-handshake-word">{{sas.omen}}</span>',
                    '        <span class="desuq-handshake-word">{{sas.strike}}</span>',
                    '        <span class="desuq-handshake-bracket">&#12299;</span>',
                    '      </div>',
                    '      <div class="desuq-handshake-rank-line">Rank {{sas.rankRoman}} <span class="desuq-handshake-dim">({{sas.rank}} of 256)</span></div>',
                    '      <div class="desuq-handshake-note">',
                    '        <p>',
                    '          <strong>This card is on their screen too.</strong>',
                    '          Compare it with them &mdash; the words, the rank and the sigil.',
                    '          If they match, this device really is theirs.',
                    '        </p>',
                    '        <p class="desuq-handshake-warning">',
                    '          <span class="fa fa-exclamation-triangle"></span>',
                    '          Compare it somewhere <em>other than</em> where you sent the device ID,',
                    '          and never inside Syncthing itself. Anyone who could swap the ID in that',
                    '          chat could swap this too. A call where you recognise each other is the',
                    '          strongest version.',
                    '        </p>',
                    // Says what the card is worth, in the place where somebody
                    // is deciding how much to trust it. The number is not
                    // decoration: 2^32 is small enough that stating it honestly
                    // is the difference between a security claim and a
                    // security theatre, and the thing that actually makes it
                    // work is in the sentence after it.
                    '        <p class="desuq-handshake-note-fine">',
                    '          The card is four bytes of a hash of both device IDs &mdash; one of about',
                    '          four billion. Somebody forging it would have to generate identities by the',
                    '          billion aimed at you and this person in particular, and finish before the',
                    '          two of you compare. What actually protects you is comparing on a channel',
                    '          they do not control; the card only gives you something short enough to',
                    '          read out.',
                    '        </p>',
                    '      </div>',
                    '      <button type="button" class="btn btn-sm desuq-handshake-btn"',
                    '              ng-if="!challenge && !confirmed" ng-click="startChallenge()">',
                    '        <span class="fa fa-shield"></span> Confirm it matches',
                    '      </button>',
                    '      <button type="button" class="btn btn-sm desuq-handshake-btn desuq-handshake-btn-undo"',
                    '              ng-if="!challenge && confirmed" ng-click="unconfirm()">',
                    '        <span class="fa fa-undo"></span> Mark as not verified',
                    '      </button>',
                    '    </div>',
                    '  </div>',

                    // --- the pick-one-of-three check ---
                    '  <div class="desuq-handshake-challenge" ng-if="challenge">',
                    '    <div ng-if="!noneMatch">',
                    '      <div class="desuq-handshake-ask">',
                    '        Which card are they looking at?',
                    '        <span class="desuq-handshake-dim">Pick the one they described. Your own card is hidden until you have.</span>',
                    '      </div>',
                    '      <div class="desuq-handshake-deal">',
                    '        <div class="desuq-handshake-slot" ng-repeat="c in challenge"',
                    '             ng-class="{\'is-wrong\': wrongPick === c.rank}"',
                    '             ng-style="{\'animation-delay\': ($index * 90) + \'ms\'}"',
                    '             ng-click="choose(c)" role="button" tabindex="0">',
                    '          <desuq-card card="c" size="sm"></desuq-card>',
                    '        </div>',
                    '      </div>',
                    '      <div class="desuq-handshake-wrong" ng-if="wrongPick">',
                    '        <span class="fa fa-times-circle"></span>',
                    '        <strong>That is not the card on this screen.</strong>',
                    '        If they really did read that out, stop &mdash; do not add this device,',
                    '        and check the device ID with them again somewhere you trust.',
                    '      </div>',
                    // Without this, somebody whose partner reads out a card
                    // that is on none of these three has no honest move: the
                    // dialogue offers three answers and every one of them is a
                    // claim that it matched. The correct outcome of a real
                    // mismatch was reachable only by picking a card at random
                    // and getting told off for it.
                    '      <button type="button" class="btn btn-sm desuq-handshake-btn desuq-handshake-btn-undo"',
                    '              ng-click="noneOfThese()">',
                    '        None of these &mdash; they read out something else',
                    '      </button>',
                    '    </div>',
                    '    <div class="desuq-handshake-wrong" ng-if="noneMatch">',
                    '      <span class="fa fa-times-circle"></span>',
                    '      <strong>Then stop, and do not add this device.</strong>',
                    '      Two cards that disagree means the device ID you were given is not the one',
                    '      they sent. Either it was altered on the way to you, or one of you pasted',
                    '      the wrong thing. Ask them for the ID again somewhere else &mdash; a call,',
                    '      in person &mdash; and compare the cards again before going any further.',
                    '    </div>',
                    '    <button type="button" class="btn btn-sm desuq-handshake-btn desuq-handshake-btn-undo" ng-click="cancelChallenge()">',
                    '      <span ng-if="!noneMatch">Cancel</span><span ng-if="noneMatch">Back to the card</span>',
                    '    </button>',
                    '  </div>',
                    '</div>'
                ].join('\n'),
                link: function (scope) {
                    function key() {
                        return String(scope.remoteId || '').toUpperCase().replace(/[^A-Z0-9]/g, '');
                    }

                    function refresh() {
                        scope.sas = handshake.of(scope.localId, scope.remoteId);
                        scope.challenge = null;
                        scope.wrongPick = null;
                        scope.noneMatch = false;
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

                    scope.startChallenge = function () {
                        if (!scope.sas) {
                            return;
                        }
                        var deck = [scope.sas].concat(handshake.decoysFor(scope.sas));
                        // Deal in an order derived from the digest, so the real
                        // card is not always in the same slot but the spread is
                        // stable for a given pair.
                        var shift = (scope.sas.digest ? scope.sas.digest[12] : 0) % deck.length;
                        scope.challenge = deck.slice(shift).concat(deck.slice(0, shift));
                        scope.wrongPick = null;
                        scope.noneMatch = false;
                    };

                    scope.cancelChallenge = function () {
                        scope.challenge = null;
                        scope.wrongPick = null;
                        scope.noneMatch = false;
                    };

                    // Nothing is recorded and nothing is undone: a device that
                    // was never confirmed stays unconfirmed, and one that was
                    // keeps its tick until somebody explicitly removes it. The
                    // job here is to say what to do, not to act on a claim
                    // this screen cannot check.
                    scope.noneOfThese = function () {
                        scope.wrongPick = null;
                        scope.noneMatch = true;
                    };

                    scope.choose = function (card) {
                        if (!scope.sas) {
                            return;
                        }
                        if (card.rank !== scope.sas.rank || card.phrase !== scope.sas.phrase) {
                            scope.wrongPick = card.rank;
                            return;
                        }
                        var map = load();
                        map[key()] = {
                            phrase: scope.sas.phrase,
                            rank: scope.sas.rank,
                            at: new Date().toLocaleDateString()
                        };
                        save(map);
                        scope.challenge = null;
                        scope.wrongPick = null;
                        scope.noneMatch = false;
                        refresh();
                        // Let the confirmed styling land after the digest, so
                        // the flourish plays rather than appearing instantly.
                        $timeout(angular.noop);
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
