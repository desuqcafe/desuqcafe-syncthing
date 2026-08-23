// Property tests for the device verification handshake.
//
//     node custom/scripts/test-handshake.js
//
// Node is not needed to build the fork, so this is a developer tool rather
// than a build step -- the build runs check-handshake-words.ps1, which needs
// nothing but PowerShell. Run this after touching anything under
// gui/default/syncthing/desuq/.
//
// It loads the real GUI files with a stub `angular` so the factories can be
// pulled out and exercised directly, which means it tests the code that
// actually ships rather than a copy of it.
//
// The two properties that matter most:
//
//   * SHA-256 agrees with Node's crypto, byte for byte. A subtly wrong hash
//     would still produce confident-looking phrases -- just not the same ones
//     on both machines, which would make every verification fail and teach
//     people to ignore it.
//   * of(A,B) === of(B,A). If the two sides disagreed on ordering they would
//     compute different phrases from the same pair, and an honest pairing
//     would look like an attack.

const fs = require('fs');
const path = require('path');
const crypto = require('crypto');

const registry = {};
global.angular = {
    module() {
        const api = {
            factory(n, d) { registry[n] = d; return api; },
            constant(n, v) { registry[n] = v; return api; },
            directive() { return api; }
        };
        return api;
    }
};

const base = path.join(__dirname, '..', '..', 'gui', 'default', 'syncthing', 'desuq');
eval(fs.readFileSync(path.join(base, 'handshakeWords.js'), 'utf8'));
eval(fs.readFileSync(path.join(base, 'deviceHandshakeDirective.js'), 'utf8'));

const sha256 = registry['desuqSha256']();
const words = registry['desuqHandshakeWords'];
const hsDef = registry['desuqHandshake'];
const handshake = hsDef[hsDef.length - 1](sha256, words);

let fails = 0;
function check(name, cond, extra) {
    if (!cond) { fails++; console.log('FAIL ' + name + (extra ? '  ' + extra : '')); }
    else { console.log('ok   ' + name + (extra ? '  ' + extra : '')); }
}

// --- SHA-256 against a known-good implementation -------------------------

const hex = b => b.map(x => x.toString(16).padStart(2, '0')).join('');
const vectors = [
    '',
    'abc',
    'The quick brown fox jumps over the lazy dog',
    'a'.repeat(55),   // longest message that still pads into one block
    'a'.repeat(56),   // first length that forces a second block
    'a'.repeat(64),
    'a'.repeat(1000),
    'h\u00e9llo w\u00f6rld'  // multi-byte UTF-8
];
let shaBad = 0;
for (const v of vectors) {
    if (hex(sha256(v)) !== crypto.createHash('sha256').update(v, 'utf8').digest('hex')) {
        shaBad++;
    }
}
check('SHA-256 matches Node crypto on ' + vectors.length + ' vectors', shaBad === 0, 'bad=' + shaBad);

// --- the handshake -------------------------------------------------------

const A = 'RZAJ5B5-27A7VV3-YKNLPEN-XNCBYFW-GCPS5Z5-G37WIZQ-A6QRJ56-WHPUXQJ';
const B = 'XJXAFN4-GWDLBZX-MWGHOJB-VLKE4ZS-DK7V6CK-Y4BZIKR-5YRQPT5-LP2ZSQF';
const C = '2FGMV2I-HEBG5DU-72FQ7O7-E6IKBAK-ICDG7YI-SYX3GXD-7BQ4XON-KXXT5Q6';

const ab = handshake.of(A, B);
const ba = handshake.of(B, A);
console.log('\n  example: \u300a ' + ab.phrase + ' \u300b Rank ' + ab.rankRoman + ' (' + ab.rank + ')\n');

check('symmetric: of(A,B) === of(B,A)', ab.phrase === ba.phrase && ab.rank === ba.rank);

const loose = handshake.of(A.replace(/-/g, ''), B.toLowerCase());
check('dashes and case are canonicalised away', loose.phrase === ab.phrase && loose.rank === ab.rank);

const ac = handshake.of(A, C);
check('a different peer gives a different phrase', ac.phrase !== ab.phrase || ac.rank !== ab.rank);

check('a device paired with itself is rejected', handshake.of(A, A) === null);
check('a half-typed ID is rejected', handshake.of(A, 'RZAJ5B5-27A7VV3') === null);
check('empty input is rejected', handshake.of(A, '') === null && handshake.of('', B) === null);
check('valid() accepts a real ID', handshake.valid(A) === true);
check('valid() rejects a short one', handshake.valid('ABC') === false);

// --- distribution --------------------------------------------------------
//
// Confirms the space really is 2^32 rather than some byte being stuck. Each
// synthetic ID comes from a hash of the counter; an arithmetic generator was
// tried first and cycled after ~544 values, which measured the generator
// instead of the handshake.

const b32 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
const seen = { aspect: new Set(), omen: new Set(), strike: new Set(), rank: new Set() };
const phrases = new Set();
const SAMPLES = 20000;
let rankBad = 0;

const roman = n => {
    const t = [[1000, 'M'], [900, 'CM'], [500, 'D'], [400, 'CD'], [100, 'C'], [90, 'XC'],
        [50, 'L'], [40, 'XL'], [10, 'X'], [9, 'IX'], [5, 'V'], [4, 'IV'], [1, 'I']];
    let out = '';
    for (const [v, s] of t) { while (n >= v) { out += s; n -= v; } }
    return out;
};

for (let i = 0; i < SAMPLES; i++) {
    const seed = crypto.createHash('sha256').update('id' + i).digest();
    let id = '';
    for (let j = 0; j < 52; j++) { id += b32[seed[j % 32] % 32]; }
    const r = handshake.of(A, id);
    if (!r) { continue; }
    seen.aspect.add(r.aspect); seen.omen.add(r.omen);
    seen.strike.add(r.strike); seen.rank.add(r.rank);
    phrases.add(r.phrase + '#' + r.rank);
    if (r.rank < 1 || r.rank > 256 || roman(r.rank) !== r.rankRoman) { rankBad++; }
}

check('rank stays in 1-256 and the numeral agrees', rankBad === 0, 'bad=' + rankBad);
for (const k of ['aspect', 'omen', 'strike', 'rank']) {
    check('all 256 values of ' + k + ' are reachable', seen[k].size === 256, seen[k].size + '/256');
}
// Birthday bound over 2^32 predicts ~0.05 collisions at this sample size, so
// anything more than a handful means the space is smaller than advertised.
check('no phrase collisions across ' + SAMPLES + ' pairs', phrases.size >= SAMPLES - 2,
    phrases.size + ' distinct');

console.log(fails === 0 ? '\nAll handshake properties hold.' : '\n' + fails + ' FAILURES');
process.exit(fails ? 1 : 0);
