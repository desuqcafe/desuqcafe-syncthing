// Renders the "does this rate limit actually apply" note through real Angular
// and asserts what somebody standing in front of the dialogue ends up reading.
//
//     npm install jsdom      (once, anywhere on the path)
//     node custom/scripts/test-lanlimit-render.js
//
// No Syncthing needed: everything this directive says is derived from four
// values, and the point of the test is the wording and the conditions, not the
// transport. The one fact it does depend on -- that /rest/system/connections
// reports isLocal, from the same IsLocal() the limiter consults -- is asserted
// against a live instance by the deployment notes, not here.

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

const dom = new JSDOM(
    '<!DOCTYPE html><html><body><div id="host" ng-controller="TestCtrl">' +
    // The settings dialogue: the checkbox is on the same screen.
    '<desuq-lan-limit id="settings" recv="recv" send="send" in-lan="inLan"></desuq-lan-limit>' +
    // The device editor: the checkbox is elsewhere, and there is a live
    // connection to report on.
    '<desuq-lan-limit id="device" recv="recv" send="send" in-lan="inLan"' +
    '   device-id="peer" connections="connections"' +
    '   elsewhere="Actions -> Settings -> Connections"></desuq-lan-limit>' +
    '</div></body></html>',
    { runScripts: 'outside-only', pretendToBeVisual: true, url: 'http://127.0.0.1:8384/' }
);

const { window } = dom;
global.window = window;
global.document = window.document;
global.navigator = window.navigator;

window.eval(fs.readFileSync(
    path.join(REPO, 'gui', 'default', 'vendor', 'angular', 'angular.js'), 'utf8'));
const angular = window.angular;
angular.module('syncthing.core', []);
window.eval(fs.readFileSync(path.join(desuq, 'lanLimitDirective.js'), 'utf8'));

const PEER = 'DOTXM4D-P5KQMPR-E4GKNVC-P56RCRR-HEFJ4NT-IDLZTFD-ETSTCJN-X7BGOAJ';

angular.module('syncthing.core').controller('TestCtrl', ['$scope', function ($scope) {
    $scope.recv = 0;
    $scope.send = 0;
    $scope.inLan = false;
    $scope.peer = PEER;
    $scope.connections = {};
}]);

angular.bootstrap(window.document.body, ['syncthing.core']);

const host = window.document.getElementById('host');
const scope = angular.element(host).scope();
const settings = () => host.querySelector('#settings').textContent.replace(/\s+/g, ' ').trim();
const device = () => host.querySelector('#device').textContent.replace(/\s+/g, ' ').trim();

function set(fn) {
    scope.$apply(fn);
}

console.log('The LAN rate-limit note');

console.log('\n-- with no limit set there is nothing to say');
check('the settings note is silent', settings() === '', JSON.stringify(settings()));
check('the device note is silent', device() === '', JSON.stringify(device()));

console.log('\n-- a limit that will not apply on the LAN');
set(() => { scope.recv = 1024; });
check('it says so, and says it plainly',
    /Not applied on the local network/.test(settings()), settings());
check('and says what is limited instead',
    /Only traffic that leaves this network is limited/.test(settings()));
check('the settings dialogue points at the checkbox below it',
    /Tick .Limit Bandwidth in LAN. below/.test(settings()), settings());
check('the device editor points at where the checkbox actually is',
    /in Actions -> Settings -> Connections/.test(device()), device());
check('and does not tell you to look below, where there is nothing',
    !/below/.test(device()));

console.log('\n-- an outgoing-only limit counts too');
set(() => { scope.recv = 0; scope.send = 2048; });
check('the note is still shown', /Not applied on the local network/.test(settings()));

console.log('\n-- what is actually happening on the wire');
check('nothing is claimed about a device that is not connected',
    !/right now/.test(device()) && !/is not on the local network/.test(device()), device());

set(() => { scope.connections[PEER] = { connected: true, isLocal: true }; });
check('a device on the LAN is named as such',
    /connected over the local network right now, so nothing is being limited/.test(device()),
    device());

set(() => { scope.connections[PEER] = { connected: true, isLocal: false }; });
check('and a device that is not gets the opposite, not silence',
    /not on the local network, so the limits are in force/.test(device()), device());

set(() => { scope.connections[PEER] = { connected: false, isLocal: true }; });
check('a stale isLocal on a dropped connection is not reported',
    !/right now/.test(device()) && !/in force/.test(device()), device());

console.log('\n-- with the checkbox ticked');
set(() => { scope.inLan = true; scope.connections[PEER] = { connected: true, isLocal: true }; });
check('the note flips to confirming rather than disappearing',
    /Applies to every connection, including devices on this network/.test(settings()),
    settings());
check('the warning is gone', !/Not applied/.test(settings()));
// The live sentence exists to explain why a limit is being skipped. With the
// checkbox on nothing is skipped, so repeating it would be noise.
check('and the live sentence is dropped, having nothing left to explain',
    !/right now/.test(device()), device());

console.log('\n-- the styling hook follows the state');
const cls = () => host.querySelector('#device').querySelector('.desuq-lanlimit').className;
check('ticked reads as confirmation', /desuq-lanlimit-on/.test(cls()), cls());
set(() => { scope.inLan = false; });
check('unticked reads as a caution', /desuq-lanlimit-off/.test(cls()), cls());

console.log('');
if (failures) {
    console.log(failures + ' check(s) failed.');
    process.exit(1);
}
console.log('All checks passed.');
