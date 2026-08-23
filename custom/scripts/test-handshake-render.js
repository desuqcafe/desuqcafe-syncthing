// Renders the device verification card through real Angular and asserts what
// the user actually ends up looking at.
//
//     npm install jsdom      (once, anywhere on the path)
//     node custom/scripts/test-handshake-render.js
//
// jsdom is not a dependency of this repository and the build does not need it,
// so this is a developer tool. test-handshake.js covers the maths with no
// dependencies at all; this one covers the template, the isolate-scope
// bindings and the confirmation flow -- the parts where a mistake still
// produces a plausible-looking card.
//
// It loads gui/default/vendor/angular/angular.js and the fork's own files
// unmodified, so it exercises exactly what ships.

const fs = require('fs');
const path = require('path');

let JSDOM;
try {
    ({ JSDOM } = require('jsdom'));
} catch (e) {
    console.error('jsdom is not installed. Run:  npm install jsdom');
    console.error('(This test is optional; test-handshake.js needs nothing.)');
    process.exit(2);
}

const REPO = path.join(__dirname, '..', '..');
const gui = path.join(REPO, 'gui', 'default');
const desuq = path.join(gui, 'syncthing', 'desuq');

const dom = new JSDOM(
    '<!DOCTYPE html><html><body>' +
    '<div id="host" ng-controller="TestCtrl">' +
    '  <device-handshake local-id="myID" remote-id="peer"></device-handshake>' +
    '  <device-handshake local-id="myID" remote-id="peer" compact="true"></device-handshake>' +
    '</div></body></html>',
    { runScripts: 'outside-only', pretendToBeVisual: true, url: 'http://127.0.0.1:8384/' }
);

const { window } = dom;
global.window = window;
global.document = window.document;
global.navigator = window.navigator;

window.eval(fs.readFileSync(path.join(gui, 'vendor', 'angular', 'angular.js'), 'utf8'));
const angular = window.angular;
angular.module('syncthing.core', []);
window.eval(fs.readFileSync(path.join(desuq, 'handshakeWords.js'), 'utf8'));
window.eval(fs.readFileSync(path.join(desuq, 'deviceHandshakeDirective.js'), 'utf8'));

const ID_SELF = 'RZAJ5B5-27A7VV3-YKNLPEN-XNCBYFW-GCPS5Z5-G37WIZQ-A6QRJ56-WHPUXQJ';
const ID_PEER = 'XJXAFN4-GWDLBZX-MWGHOJB-VLKE4ZS-DK7V6CK-Y4BZIKR-5YRQPT5-LP2ZSQF';
const ID_OTHER = '2FGMV2I-HEBG5DU-72FQ7O7-E6IKBAK-ICDG7YI-SYX3GXD-7BQ4XON-KXXT5Q6';

angular.module('syncthing.core').controller('TestCtrl', ['$scope', function ($scope) {
    $scope.myID = ID_SELF;
    $scope.peer = ID_PEER;
}]);

angular.bootstrap(window.document.body, ['syncthing.core']);

const host = window.document.getElementById('host');
// Address the <device-handshake> elements rather than the card divs inside
// them. The card sits under an ng-if, so it is destroyed and recreated
// whenever the pair changes, and a reference captured once goes stale.
const els = host.querySelectorAll('device-handshake');
const rootScope = angular.element(host).scope();

const card = el => el.querySelector('.desuq-handshake');
const txt = (el, sel) => {
    const c = card(el);
    const n = c && c.querySelector(sel);
    return n ? n.textContent.replace(/\s+/g, ' ').trim() : null;
};

let fails = 0;
function check(name, cond, extra) {
    if (!cond) { fails++; console.log('FAIL ' + name + (extra ? '  ' + extra : '')); }
    else { console.log('ok   ' + name + (extra ? '  ' + extra : '')); }
}

check('both directives rendered a card', els.length === 2 && !!card(els[0]) && !!card(els[1]));

const full = els[0];
const compactEl = els[1];
const phrase = txt(full, '.desuq-handshake-phrase');
const rankLine = txt(full, '.desuq-handshake-rank-line');
const cardWords = txt(full, '.desuq-card-name');
const cardRank = txt(full, '.desuq-card-rank');
const tier = txt(full, '.desuq-card-tier');

console.log('');
console.log('  card words : ' + cardWords);
console.log('  card rank  : ' + cardRank + '   tier: ' + tier);
console.log('  phrase     : ' + phrase);
console.log('  rank line  : ' + rankLine);
console.log('');

// The known answer for ID_SELF paired with ID_PEER, written out rather than
// asked of the module, so that this asserts the whole pipeline -- hash, word
// lookup, rank, template -- rather than that the template agrees with itself.
// Change the fixture IDs above and these four values must be recomputed; the
// test prints what it got, so the run that fails also tells you the answer.
var WANT_WORDS = ['AWAKENED', 'ANNAL', 'UMBRAGRASP'];
var WANT_RANK_ROMAN = 'CXXIII';
var WANT_RANK_NUMBER = '123';

check('phrase shows all three words',
    WANT_WORDS.every(function (w) { return phrase.indexOf(w) >= 0; }), phrase);
check('decorative brackets rendered', phrase.indexOf('《') >= 0 && phrase.indexOf('》') >= 0);
check('rank shown as numeral and as a number',
    rankLine.indexOf(WANT_RANK_ROMAN) >= 0 && rankLine.indexOf(WANT_RANK_NUMBER) >= 0, rankLine);

// The card face must carry the same identity as the text beside it, or the
// two could disagree and there would be no way to tell which was right.
check('card face repeats the same three words',
    WANT_WORDS.every(function (w) { return cardWords.indexOf(w) >= 0; }), cardWords);
check('card face repeats the same rank', cardRank === WANT_RANK_ROMAN, cardRank);
check('card face names a rarity tier', /^(COMMON|RARE|EPIC|LEGENDARY)$/.test(tier), tier);

// The sigil is the third encoding of the same bytes.
const sigil = card(full).querySelector('.desuq-sigil-svg');
check('sigil rendered as SVG', !!sigil);
check('sigil has a star, a core and orbit marks',
    !!sigil.querySelector('.desuq-sigil-star') &&
    !!sigil.querySelector('.desuq-sigil-core') &&
    sigil.querySelectorAll('.desuq-sigil-mark').length >= 3);

// The honesty requirement. A verification ritual people perform incorrectly is
// worse than none, so the card has to say what does and does not count.
const note = txt(full, '.desuq-handshake-note');
check('warns against the channel that carried the ID', /other than.*where you sent the device ID/i.test(note));
check('warns against comparing inside Syncthing', /never inside Syncthing/i.test(note));
check('explains that the phrase could be swapped too', /could swap this too/i.test(note));
check('does not prescribe voice as the only channel', !/verify by voice/i.test(note));

// Compact form is for the device panel: the card, nothing else.
check('compact form shows a card', !!card(compactEl).querySelector('.desuq-card'));
check('compact form omits the explanation', !card(compactEl).querySelector('.desuq-handshake-note'));
check('compact form omits the confirm button', !card(compactEl).querySelector('.desuq-handshake-btn'));
check('compact form carries the compact class', card(compactEl).classList.contains('desuq-handshake-compact'));
check('hero card renders at medium size', !!card(full).querySelector('.desuq-card-md'));
check('compact card renders at small size', !!card(compactEl).querySelector('.desuq-card-sm'));

// --- the pick-one-of-three check ---

const iso = angular.element(full).isolateScope();
check('starts unconfirmed', iso.confirmed === false);
check('no cards dealt until asked', !card(full).querySelector('.desuq-handshake-deal'));

iso.$apply(function () { iso.startChallenge(); });
const slots = card(full).querySelectorAll('.desuq-handshake-slot');
check('deals exactly three cards', slots.length === 3, slots.length + ' dealt');

const dealt = iso.challenge;
check('one dealt card is the real one',
    dealt.filter(c => c.phrase === iso.sas.phrase && c.rank === iso.sas.rank).length === 1);

// A decoy sharing any of the four positions with the real card would make the
// check ambiguous -- someone comparing only the rank, or only the last word,
// could pick a decoy and be told they were right.
const decoys = dealt.filter(c => c.rank !== iso.sas.rank);
check('two decoys, distinct from each other', decoys.length === 2 && decoys[0].rank !== decoys[1].rank);
check('no decoy shares any word or the rank with the real card',
    decoys.every(d => d.aspect !== iso.sas.aspect && d.omen !== iso.sas.omen &&
                      d.strike !== iso.sas.strike && d.rank !== iso.sas.rank));

// Stability: the same pair must always deal the same spread. A hand that
// reshuffled on every redraw would look like the phrase itself was unstable.
const before = dealt.map(c => c.rank).join(',');
iso.$apply(function () { iso.cancelChallenge(); iso.startChallenge(); });
check('the same pair always deals the same spread', iso.challenge.map(c => c.rank).join(',') === before);

// Picking a decoy must refuse, and must not confirm anything.
iso.$apply(function () { iso.choose(decoys[0]); });
check('picking a decoy is refused', iso.confirmed === false && iso.wrongPick === decoys[0].rank);
check('a refusal explains what to do', /do not add this device/i.test(txt(full, '.desuq-handshake-wrong') || ''));
check('the cards stay on the table after a wrong pick', !!iso.challenge);

// Picking the real card confirms.
iso.$apply(function () { iso.choose(iso.sas); });
check('picking the real card confirms', iso.confirmed === true);
check('the challenge is cleared once confirmed', !iso.challenge);
check('confirmed state reaches the DOM', card(full).classList.contains('desuq-handshake-confirmed'));
check('confirmation is persisted', !!window.localStorage.getItem('desuq.handshake.confirmed'));

// A stored confirmation must not survive the pair changing. Otherwise pasting
// a different device ID would inherit a tick that was never earned for it --
// an actively dangerous bug, not a cosmetic one.
rootScope.$apply(function () { rootScope.peer = ID_OTHER; });
check('changing the peer clears the confirmation', iso.confirmed === false);
check('changing the peer changes the phrase',
    txt(full, '.desuq-handshake-phrase') !== phrase, 'now: ' + txt(full, '.desuq-handshake-phrase'));

rootScope.$apply(function () { rootScope.peer = ID_PEER; });
check('returning to a confirmed pair restores the tick', iso.confirmed === true);

// Half-typed input must render nothing rather than a card built from a partial
// ID, which would show a phrase that means nothing.
rootScope.$apply(function () { rootScope.peer = 'RZAJ5B5'; });
check('a half-typed ID renders no card at all', card(full) === null);

rootScope.$apply(function () { rootScope.peer = ID_SELF; });
check('a device paired with itself renders no card', card(full) === null);

console.log(fails === 0 ? '\nCard renders correctly.' : '\n' + fails + ' FAILURES');
process.exit(fails ? 1 : 0);
