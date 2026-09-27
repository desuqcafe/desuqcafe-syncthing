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
    '  <device-handshake local-id="myID" remote-id="peer" device="dev" peer-name="Kai"></device-handshake>' +
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
// app.js defines this; the verification service reads it like every GUI file.
window.urlbase = 'rest';
window.eval(fs.readFileSync(path.join(desuq, 'handshakeWords.js'), 'utf8'));
window.eval(fs.readFileSync(path.join(desuq, 'deviceHandshakeDirective.js'), 'utf8'));

const ID_SELF = 'RZAJ5B5-27A7VV3-YKNLPEN-XNCBYFW-GCPS5Z5-G37WIZQ-A6QRJ56-WHPUXQJ';
const ID_PEER = 'XJXAFN4-GWDLBZX-MWGHOJB-VLKE4ZS-DK7V6CK-Y4BZIKR-5YRQPT5-LP2ZSQF';
const ID_OTHER = '2FGMV2I-HEBG5DU-72FQ7O7-E6IKBAK-ICDG7YI-SYX3GXD-7BQ4XON-KXXT5Q6';

// The config, as far as verification touches it: ID_PEER is configured and
// not verified; ID_OTHER is not configured at all -- somebody being added.
const config = { [ID_PEER]: { deviceID: ID_PEER, name: 'Kai', desuqVerifiedAt: '' } };
const patches = [];
angular.module('syncthing.core')
    .factory('$http', ['$q', function ($q) {
        const devOf = url => decodeURIComponent(url.replace(/^rest\/config\/devices\//, ''));
        const http = function () { return $q.when({ data: {} }); };
        http.get = function (url) {
            const d = config[devOf(url)];
            return d ? $q.when({ data: angular.copy(d) }) : $q.reject({ status: 404, data: 'no such device' });
        };
        http.patch = function (url, body) {
            const d = config[devOf(url)];
            if (!d) { return $q.reject({ status: 404 }); }
            patches.push({ id: d.deviceID, body: body });
            Object.assign(d, body);
            return $q.when({ data: {} });
        };
        return http;
    }])
    .controller('TestCtrl', ['$scope', function ($scope) {
        $scope.myID = ID_SELF;
        $scope.peer = ID_PEER;
        // What a modal would save afterwards -- the card mirrors onto it.
        $scope.dev = { deviceID: ID_PEER };
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
// worse than none, so the card has to say what does and does not count. The
// long form is folded under "Why this matters", but it is in the DOM.
const note = txt(full, '.desuq-handshake-note');
check('warns against the channel that carried the ID', /other than.*where you sent the device ID/i.test(note));
check('warns against comparing inside Syncthing', /never inside Syncthing/i.test(note));
check('explains that the phrase could be swapped too', /could swap this too/i.test(note));
check('does not prescribe voice as the only channel', !/verify by voice/i.test(note));
check('says how much entropy the card carries', /four billion/i.test(note));
check('says the channel is what protects you, not the card',
    /channel they do not control/i.test(note));
check('the explanation is folded, not in front of the button',
    !!card(full).querySelector('details.desuq-handshake-why .desuq-handshake-warning'));

// The steps, in the order they are done, and the rule they exist to teach:
// both people pick, each on their own computer.
const steps = card(full).querySelectorAll('.desuq-handshake-steps li');
check('three numbered steps', steps.length === 3, steps.length + ' steps');
check('step 1 says to call, not to use the chat', /call/i.test(steps[0].textContent) && /not the chat/i.test(steps[0].textContent));
check('step 3 says to swap, each on their own computer', /swap/i.test(steps[2].textContent) && /own computer/i.test(steps[2].textContent));
check('the person is named, not "them"', /Kai/.test(steps[0].textContent));
check('says nothing syncs until verified',
    /nothing syncs with Kai until/i.test(txt(full, '.desuq-handshake-status') || ''));
const go = card(full).querySelector('.desuq-handshake-btn-go');
check('the button says what is happening, not "confirm"', !!go && /Kai is reading me their card/.test(go.textContent),
    go ? go.textContent.trim() : '');

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

// The hero has to go while the check is running. It carries the real card, the
// phrase written out in words and the rank in two notations; with it on screen
// the pick-one-of-three is a matching exercise that anybody passes without
// having listened to the other person at all, which is the one thing the check
// exists to require.
check('the real card is hidden while choosing', !card(full).querySelector('.desuq-handshake-hero'));
check('the phrase is hidden while choosing', !card(full).querySelector('.desuq-handshake-phrase'));
check('the rank is hidden while choosing', !card(full).querySelector('.desuq-handshake-rank-line'));

iso.$apply(function () { iso.cancelChallenge(); });
check('cancelling brings the card back', !!card(full).querySelector('.desuq-handshake-hero'));
check('cancelling clears the deal', !card(full).querySelector('.desuq-handshake-deal'));
iso.$apply(function () { iso.startChallenge(); });

// Somebody whose partner reads out a card that is on none of these three needs
// a move that is not a claim it matched. Without one, the only route to the
// correct outcome is guessing wrong on purpose.
iso.$apply(function () { iso.noneOfThese(); });
check('"none of these" does not confirm', iso.confirmed === false);
check('"none of these" takes the cards away', !card(full).querySelector('.desuq-handshake-deal'));
check('"none of these" says to stop',
    /do not add this device/i.test(txt(full, '.desuq-handshake-wrong') || ''));
check('"none of these" says to re-check the ID elsewhere',
    /ask them for the ID again somewhere else/i.test(txt(full, '.desuq-handshake-wrong') || ''));
check('the real card stays hidden after "none of these"',
    !card(full).querySelector('.desuq-handshake-hero'));
iso.$apply(function () { iso.cancelChallenge(); iso.startChallenge(); });
check('starting over clears the mismatch', iso.noneMatch === false);

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

// Picking the real card confirms -- and writes it to the config, because the
// server refuses the device until it is there (lib/model/desuq_verified.go).
iso.$apply(function () { iso.choose(iso.sas); });
check('picking the real card confirms', iso.confirmed === true);
check('the challenge is cleared once confirmed', !iso.challenge);
check('confirmed state reaches the DOM', card(full).classList.contains('desuq-handshake-confirmed'));
check('the verification is written to the device config',
    !!config[ID_PEER].desuqVerifiedAt && patches.length === 1, JSON.stringify(patches));
check('it is written as a time', !isNaN(Date.parse(config[ID_PEER].desuqVerifiedAt)));
check('it is mirrored onto the object a modal will save', rootScope.dev.desuqVerifiedAt === config[ID_PEER].desuqVerifiedAt);
check('nothing is kept in localStorage any more', window.localStorage.getItem('desuq.handshake.confirmed') === null);
check('once verified, the other side is reminded to pick too',
    /also has to pick your card/i.test(txt(full, '.desuq-handshake-after') || ''));

// A different device must not inherit a tick that was never earned for it --
// an actively dangerous bug, not a cosmetic one.
rootScope.$apply(function () { rootScope.peer = ID_OTHER; });
check('changing the peer clears the confirmation', iso.confirmed === false);
check('changing the peer changes the phrase',
    txt(full, '.desuq-handshake-phrase') !== phrase, 'now: ' + txt(full, '.desuq-handshake-phrase'));
check('and clears the mirrored value', rootScope.dev.desuqVerifiedAt === '');

rootScope.$apply(function () { rootScope.peer = ID_PEER; });
check('returning to a verified device reads the tick back from the config', iso.confirmed === true);

// Somebody not in the config yet -- being added right now. Verified in
// memory, for whoever saves them to read back; nothing to PATCH.
rootScope.$apply(function () { rootScope.peer = ID_OTHER; });
iso.$apply(function () { iso.choose(iso.sas); });
const verification = angular.element(host).injector().get('desuqVerification');
check('a device not yet saved is verified provisionally', iso.confirmed === true && !!verification.provisional(ID_OTHER));
check('and nothing was written for it', patches.length === 1);
check('the provisional value is what a modal will save', rootScope.dev.desuqVerifiedAt === verification.provisional(ID_OTHER));

// Taking it away is a write too: it disconnects them.
rootScope.$apply(function () { rootScope.peer = ID_PEER; });
iso.$apply(function () { iso.unconfirm(); });
check('"Mark as not verified" clears the config', config[ID_PEER].desuqVerifiedAt === '' && iso.confirmed === false);

// The one-off move of confirmations made while they lived in localStorage.
const handshake = angular.element(host).injector().get('desuqHandshake');
const sas = handshake.of(ID_SELF, ID_PEER);
window.localStorage.setItem('desuq.handshake.confirmed', JSON.stringify({
    [ID_PEER.replace(/-/g, '')]: { phrase: sas.phrase, rank: sas.rank, at: '1/1/2026' }
}));
let moved = null;
rootScope.$apply(function () {
    verification.migrate(ID_SELF, [config[ID_PEER]]).then(function (n) { moved = n; });
});
check('an old confirmation is carried into the config', moved === 1 && !!config[ID_PEER].desuqVerifiedAt);
check('and the old store is removed', window.localStorage.getItem('desuq.handshake.confirmed') === null);

config[ID_PEER].desuqVerifiedAt = '';
window.localStorage.setItem('desuq.handshake.confirmed', JSON.stringify({
    [ID_PEER.replace(/-/g, '')]: { phrase: 'SOMETHING ELSE', rank: sas.rank, at: '1/1/2026' }
}));
rootScope.$apply(function () {
    verification.migrate(ID_SELF, [config[ID_PEER]]).then(function (n) { moved = n; });
});
check('a record for a different phrase is not carried over', moved === 0 && config[ID_PEER].desuqVerifiedAt === '');

// Half-typed input must render nothing rather than a card built from a partial
// ID, which would show a phrase that means nothing.
rootScope.$apply(function () { rootScope.peer = 'RZAJ5B5'; });
check('a half-typed ID renders no card at all', card(full) === null);

rootScope.$apply(function () { rootScope.peer = ID_SELF; });
check('a device paired with itself renders no card', card(full) === null);

console.log(fails === 0 ? '\nCard renders correctly.' : '\n' + fails + ' FAILURES');
process.exit(fails ? 1 : 0);
