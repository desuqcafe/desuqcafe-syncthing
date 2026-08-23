// Drives the first-run wizard through real Angular and asserts both halves of
// it: when it decides to appear, and what somebody standing in front of it
// actually reads.
//
//     npm install jsdom      (once, anywhere on the path)
//     node custom/scripts/test-wizard-render.js
//
// No Syncthing needed. The wizard reads its state over REST, so $http is
// replaced with a stub serving canned responses -- which is the point of it
// reading state over REST rather than off syncthingController's scope: the
// whole feature is exercisable without standing up the controller, and the
// canned responses are the real endpoint shapes.
//
// The service half matters more than it looks. Three of the four steps cannot
// be completed by the person in front of the screen -- they complete when
// somebody else adds them, calls them, or shares a folder -- so "does it open,
// does it stay closed once finished, does it come back where it left off" is
// the entire user-visible behaviour on most days.

const fs = require('fs');
const path = require('path');

let JSDOM;
try {
    ({ JSDOM } = require('jsdom'));
} catch (e) {
    console.error('jsdom is not installed. Run:  npm install jsdom');
    process.exit(2);
}

const REPO = path.join(__dirname, '..', '..');
const desuq = path.join(REPO, 'gui', 'default', 'syncthing', 'desuq');

let failures = 0;
function check(name, ok, detail) {
    console.log((ok ? '  ok   ' : '  FAIL ') + name + (detail ? '   ' + detail : ''));
    if (!ok) { failures++; }
}

const MY_ID = 'DOTXM4D-P5KQMPR-E4GKNVC-P56RCRR-HEFJ4NT-IDLZTFD-ETSTCJN-X7BGOAJ';
const PEER_ID = 'W3QCFTB-3CVDCU5-6VDBLXB-K4VVQGO-BQKUMVN-VZUCTGP-C4WUMYX-U5RUBQU';

// --- the world the stub serves -------------------------------------------
//
// Mutated between assertions to stand for the things that happen to somebody
// during onboarding: being added, confirming a card, being offered a folder.
const world = {
    myName: 'Alex',
    devices: [],
    folders: [],
    pendingDevices: {},
    pendingFolders: {},
    connections: {}
};

function reset() {
    world.myName = 'Alex';
    world.devices = [];
    world.folders = [];
    world.pendingDevices = {};
    world.pendingFolders = {};
    world.connections = {};
    store.clear();
}

const dom = new JSDOM(
    '<!DOCTYPE html><html><body><div id="host"></div></body></html>',
    { runScripts: 'outside-only', pretendToBeVisual: true, url: 'http://127.0.0.1:8384/' }
);

const { window } = dom;
global.window = window;
global.document = window.document;
global.navigator = window.navigator;

// app.js defines this; the wizard reads it the same way every other GUI file
// does. Loading app.js here would drag in the whole application.
window.urlbase = 'rest';

window.eval(fs.readFileSync(
    path.join(REPO, 'gui', 'default', 'vendor', 'angular', 'angular.js'), 'utf8'));
const angular = window.angular;
angular.module('syncthing.core', []);
window.eval(fs.readFileSync(path.join(desuq, 'firstRunWizard.js'), 'utf8'));

// --- the stubs -----------------------------------------------------------

// $window is deliberately left alone. jsdom provides a real localStorage and a
// navigator with no `clipboard` on it, which is exactly the environment this
// has to work in -- and replacing $window wholesale breaks $browser, which
// wants a real location and history off it.
angular.module('syncthing.core')
    .factory('$http', function ($q) {
        function reply(data) { return $q.when({ data: data }); }

        const routes = {
            'rest/system/status': () => reply({ myID: MY_ID }),
            'rest/config': () => reply({
                devices: [{ deviceID: MY_ID, name: world.myName }].concat(world.devices),
                folders: world.folders
            }),
            'rest/cluster/pending/devices': () => reply(world.pendingDevices),
            'rest/cluster/pending/folders': () => reply(world.pendingFolders),
            'rest/system/connections': () => reply({ connections: world.connections })
        };

        const http = function () { return $q.when({ data: {} }); };
        http.get = function (url, config) {
            // $templateRequest passes `cache: $templateCache` to $http and does
            // not consult the cache itself, so a stub that ignores the option
            // sends every template off to the REST API. Honouring it here is
            // not politeness, it is the difference between this working and
            // asking Syncthing for an HTML file.
            if (config && config.cache && config.cache.get(url) !== undefined) {
                return $q.when({ data: config.cache.get(url) });
            }
            const fn = routes[url];
            if (!fn) { throw new Error('unstubbed GET ' + url); }
            return fn();
        };
        http.patch = function (url, body) {
            // The only write the wizard makes.
            if (url.indexOf('rest/config/devices/') === 0) {
                world.myName = body.name;
                return $q.when({ data: {} });
            }
            throw new Error('unstubbed PATCH ' + url);
        };
        return http;
    });

// --- harness -------------------------------------------------------------

const injector = angular.injector(['ng', 'syncthing.core']);
const store = window.localStorage;
const svc = injector.get('desuqWizard');
const $rootScope = injector.get('$rootScope');
const $compile = injector.get('$compile');
const $templateCache = injector.get('$templateCache');

$templateCache.put('syncthing/desuq/firstRunWizardView.html',
    fs.readFileSync(path.join(desuq, 'firstRunWizardView.html'), 'utf8'));

// $q promises settle on a digest, so every step here has to run one.
function flush() { $rootScope.$digest(); }

function open(force) {
    let opened = null;
    svc.open(force).then(function (r) { opened = r; });
    flush();
    return opened;
}

const st = svc.state;

console.log('The first-run wizard');

// =========================================================================
console.log('\n-- when it decides to show itself');
// =========================================================================

reset();
check('opens on a fresh install', open(false) === true && st.open === true);
check('starts on the first step', svc.stepName() === 'name', 'step: ' + svc.stepName());
check('picked up the seeded device name', st.nameDraft === 'Alex', st.nameDraft);

// Somebody who already has a peer found their own way here. A wizard over the
// top of a working setup is an interruption, not help.
reset();
world.devices = [{ deviceID: PEER_ID, name: 'amy' }];
svc.state.open = false;
check('stays shut when a device is already set up', open(false) === false);
check('...but opens when asked for by hand', open(true) === true);
svc.close();

reset();
world.folders = [{ id: 'assets', label: 'Assets' }];
svc.state.open = false;
check('stays shut when a folder is already set up', open(false) === false);
svc.close();

// =========================================================================
console.log('\n-- finishing, and coming back');
// =========================================================================

reset();
open(false);
svc.goTo(2);
svc.close();
check('closing remembers the step', JSON.parse(store.getItem('desuq.wizard')).step === 2);
check('closing does not mark it finished',
    !JSON.parse(store.getItem('desuq.wizard')).completed);
check('reopening by hand comes back to that step',
    open(true) === true && st.step === 2, 'step index: ' + st.step);
svc.close();

svc.finish();
check('Done marks it completed', JSON.parse(store.getItem('desuq.wizard')).completed === true);
check('...and it never opens itself again', open(false) === false);
check('...but Actions -> Setup guide still works', open(true) === true);
svc.close();

// =========================================================================
console.log('\n-- the ticks track the real state of the world');
// =========================================================================

reset();
open(true);
check('name is done (it was seeded)', svc.isDone('name') === true);
check('code is not done with nobody added', svc.isDone('code') === false);
check('verify is not done', svc.isDone('verify') === false);
check('sync is not done', svc.isDone('sync') === false);

// A device asking to connect counts: the code has clearly reached somebody.
world.pendingDevices = { [PEER_ID]: { name: 'amy' } };
svc.refresh(); flush();
check('code ticks once somebody is knocking', svc.isDone('code') === true);

world.pendingDevices = {};
world.devices = [{ deviceID: PEER_ID, name: 'amy' }];
world.connections = { [PEER_ID]: { connected: true, clientVersion: 'v2.1.4-desuq.2' } };
svc.refresh(); flush();
check('code stays ticked once they are added', svc.isDone('code') === true);
check('the peer is listed', st.peers.length === 1 && st.peers[0].name === 'amy');
check('and shown as connected', st.peers[0].connected === true);

// The handshake directive's own store, read but never written here.
store.setItem('desuq.handshake.confirmed', JSON.stringify({
    [PEER_ID.replace(/[^A-Z0-9]/g, '')]: { phrase: 'X', rank: 1, at: 'today' }
}));
svc.refresh(); flush();
check('verify ticks off a stored confirmation', svc.isDone('verify') === true);

world.folders = [{ id: 'assets', label: 'Assets' }];
svc.refresh(); flush();
check('sync ticks once a folder exists', svc.isDone('sync') === true);
check('and then everything is done', svc.allDone() === true);

// =========================================================================
console.log('\n-- renaming the machine');
// =========================================================================

reset();
open(true);
st.nameDraft = 'Studio laptop';
svc.saveName();
flush();
check('the name is written back', world.myName === 'Studio laptop');
check('and it moves on to the code step', svc.stepName() === 'code');

// =========================================================================
console.log('\n-- what it actually says');
// =========================================================================

reset();
world.devices = [];
open(true);
flush();

// The template is compiled directly against the scope the directive builds,
// rather than through the directive itself. The directive's own link function
// is Bootstrap modal plumbing -- show, hide, wait out an overlapping modal --
// which needs a real browser and a real Bootstrap to mean anything. What is
// worth asserting here is the wording, and that is all in the template.
const scope = $rootScope.$new();
scope.st = svc.state;
scope.svc = svc;
scope.onAcceptFolder = function () { scope.accepted = true; };
scope.acceptOffer = function () { scope.accepted = true; };
const el = $compile(
    $templateCache.get('syncthing/desuq/firstRunWizardView.html'))(scope);
flush();

// The template opens with a comment, so the compiled collection's first node
// is that comment rather than the modal.
const root = Array.prototype.slice.call(el)
    .filter(function (n) { return n.nodeType === 1; })[0];

const text = () => root.textContent.replace(/\s+/g, ' ').trim();

// The rail is always drawn, so its four labels are always in the text; the
// step bodies are what ng-if switches. Assertions below are on body wording
// distinctive enough not to collide with a rail label.
svc.goTo(0); flush();
check('step 1 explains what the name is for',
    /name the other people on your team will see/i.test(text()));

svc.goTo(1); flush();
check('step 2 shows the device ID in full', text().indexOf(MY_ID) >= 0);
check('step 2 says the code is the address',
    /there is no account and no server/i.test(text()));
check('step 2 says it is waiting on a person',
    /Waiting for someone to add you/i.test(text()));
check('step 2 says how to get back here',
    /Setup guide/i.test(text()));
check('step 2 renders the QR from upstream\'s own endpoint',
    (root.querySelector('.desuq-wiz-qr') || {}).getAttribute('src') === 'qr/?text=' + MY_ID,
    (root.querySelector('.desuq-wiz-qr') || {}).getAttribute('src'));

svc.goTo(2); flush();
check('step 3 says mutual adding is not proof of identity',
    /does not prove whose code it was/i.test(text()));
check('step 3 says there is nothing to compare yet',
    /Nobody has been added yet/i.test(text()));

// This is the sentence the whole handshake depends on, and the one people get
// wrong. If it ever stops rendering, the ritual is worse than useless.
world.devices = [{ deviceID: PEER_ID, name: 'amy' }];
svc.refresh(); flush();
check('step 3 says to compare on a call, not in the chat',
    /Read the card to each other on a call/i.test(text()) &&
    /Not in the chat that carried the code/i.test(text()));

svc.goTo(3); flush();
check('step 4 promises nothing downloads before you choose',
    /Accepting it does not start downloading/i.test(text()));
check('step 4 says nothing has been offered yet',
    /No folder has been offered yet/i.test(text()));

world.pendingFolders = { assets: { offeredBy: { [PEER_ID]: { label: 'Studio assets' } } } };
scope.pendingFolders = world.pendingFolders;
svc.refresh(); flush();
check('step 4 names an offer when one arrives',
    /Studio assets/.test(text()) && /offered by amy/i.test(text()));

// =========================================================================
console.log('\n-- the copy button works without a secure context');
// =========================================================================

// navigator.clipboard is undefined at anything but 127.0.0.1 over http, and
// the GUI is routinely opened at a LAN address from the machine next door --
// the same trap that made the handshake carry its own SHA-256. The
// execCommand path is not a legacy fallback here, it is the one that runs.
let copiedText = null;
window.document.execCommand = function (cmd) {
    if (cmd === 'copy') {
        const ta = window.document.querySelector('textarea');
        copiedText = ta ? ta.value : null;
        return true;
    }
    return false;
};
window.document.queryCommandSupported = function () { return true; };

const host = window.document.getElementById('host');
const ok = svc.copy({ currentTarget: host }, MY_ID);
check('copy falls back to execCommand and copies the ID',
    ok === true && copiedText === MY_ID, copiedText);
check('the scratch textarea is cleaned up',
    host.querySelector('textarea') === null);

// =========================================================================
console.log('');

// The wizard polls while it is open, on $timeout -- which is a real timer on
// the jsdom window and will hold node open forever. Closing it is also the
// assertion that closing stops the polling.
svc.close();
window.close();

if (failures > 0) {
    console.log(failures + ' check(s) failed');
    process.exit(1);
}
console.log('all checks passed');
