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

const ID_SELF = 'A4F454V-3C73UF5-BDUQ6CH-Y4DTSND-B4ODDZK-F3E3CL3-36TLXUE-RZ47VQM';
const ID_PEER = 'ONJMGXO-DTKYWNV-5VVAL6R-SWIPFSU-RRGPGHW-IKPOTDO-XDFBAEK-UJP6DAW';
const ID_OTHER = '5GPBTOE-SWYHRWH-J3HOAG7-YPLFEYX-LTWC5OA-GLHVAYJ-7TFKA4B-254GHQY';

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
const numeral = txt(full, '.desuq-handshake-rank-numeral');

console.log('');
console.log('  phrase : ' + phrase);
console.log('  rank   : ' + rankLine);
console.log('  badge  : ' + numeral);
console.log('');

check('phrase shows all three words',
    phrase.indexOf('CRIMSON') >= 0 && phrase.indexOf('TALISMAN') >= 0 && phrase.indexOf('NOCTURNE') >= 0, phrase);
check('decorative brackets rendered',
    phrase.indexOf('《') >= 0 && phrase.indexOf('》') >= 0);
check('rank shown as numeral and as a number',
    rankLine.indexOf('CLXXVI') >= 0 && rankLine.indexOf('176') >= 0, rankLine);
check('badge shows the numeral', numeral === 'CLXXVI', numeral);

// The honesty requirement. A verification ritual people perform incorrectly is
// worse than none, so the card has to say what does and does not count.
const note = txt(full, '.desuq-handshake-note');
check('warns that comparing in-app proves nothing', /inside.*Syncthing.*proves nothing/i.test(note));
check('directs the user to a voice call', /call the other person/i.test(note));
check('explains that the phrase could be swapped too', /could swap this too/i.test(note));

// Compact form is for the device panel: the card, nothing else.
check('compact form shows the phrase', !!txt(compactEl, '.desuq-handshake-phrase'));
check('compact form omits the explanation', !card(compactEl).querySelector('.desuq-handshake-note'));
check('compact form omits the confirm button', !card(compactEl).querySelector('.desuq-handshake-btn'));
check('compact form carries the compact class', card(compactEl).classList.contains('desuq-handshake-compact'));

// --- the confirmation flow ---

const iso = angular.element(full).isolateScope();
check('starts unconfirmed', iso.confirmed === false);

iso.$apply(function () { iso.confirm(); });
check('confirm() sets the flag', iso.confirmed === true);
check('confirmed state reaches the DOM', card(full).classList.contains('desuq-handshake-confirmed'));
check('confirmation is persisted', !!window.localStorage.getItem('desuq.handshake.confirmed'));

// A stored confirmation must not survive the pair changing. Otherwise pasting
// a different device ID would inherit a tick that was never earned for it --
// which would be an actively dangerous bug, not a cosmetic one.
rootScope.$apply(function () { rootScope.peer = ID_OTHER; });
check('changing the peer clears the confirmation', iso.confirmed === false);
check('changing the peer changes the phrase',
    txt(full, '.desuq-handshake-phrase') !== phrase, 'now: ' + txt(full, '.desuq-handshake-phrase'));

rootScope.$apply(function () { rootScope.peer = ID_PEER; });
check('returning to a confirmed pair restores the tick', iso.confirmed === true);

// Half-typed input must render nothing rather than a card built from a partial
// ID, which would show a phrase that means nothing.
rootScope.$apply(function () { rootScope.peer = 'A4F454V'; });
check('a half-typed ID renders no card at all', card(full) === null);

rootScope.$apply(function () { rootScope.peer = ID_SELF; });
check('a device paired with itself renders no card', card(full) === null);

console.log(fails === 0 ? '\nCard renders correctly.' : '\n' + fails + ' FAILURES');
process.exit(fails ? 1 : 0);
