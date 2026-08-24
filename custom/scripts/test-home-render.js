// Drives the main screen through real Angular against canned REST, and asserts
// the headline rules directly.
//
//     npm install jsdom      (once, anywhere on the path)
//     node custom/scripts/test-home-render.js
//
// No Syncthing needed. The screen reads its whole state over REST, so $http is
// the only thing that has to be stubbed -- the same shape as
// test-wizard-render.js, and the reason home.js reads REST rather than
// syncthingController's scope in the first place.
//
// Two things are worth testing here and they are tested separately.
//
// The RULES: which sentence appears, in which tone, for a given state of the
// world. These are a pure function of (folders, peers, errors), exported as
// desuqHome._headline, so they are asserted directly -- no DOM, no digest, one
// call per case. This is where the real risk lives: the first version of this
// screen reported "Everything is here, in step with Yuki" about a folder Yuki
// had never accepted, because it only ever looked at local state.
//
// The RENDER: that those rules actually reach the screen, that the quiet tones
// stay quiet, and that the people strip puts the right class on the right
// person. That needs the directive compiled against a live scope.

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

const MY_ID = 'AAAAAAA-BBBBBBB-CCCCCCC-DDDDDDD-EEEEEEE-FFFFFFF-GGGGGGG-HHHHHHH';
const YUKI  = 'YJZXQAR-36FJMNA-YFM2FKV-KQHIP52-NT6INEO-RZFO6WU-Z5AMM2P-LVWLRQO';
const ANA   = '66QIQIE-NLTH3HQ-FRZOD7P-VXOL2W6-3YVTFNU-MQI4LDY-DHROTUM-CFXBZAO';

const dom = new JSDOM(
    '<!DOCTYPE html><html><body><div id="host"></div></body></html>',
    { runScripts: 'outside-only', pretendToBeVisual: true, url: 'http://127.0.0.1:8384/' }
);

const { window } = dom;
global.window = window;
global.document = window.document;
global.navigator = window.navigator;

// app.js defines this; home.js reads it the same way every other GUI file does.
window.urlbase = 'rest';

window.eval(fs.readFileSync(
    path.join(REPO, 'gui', 'default', 'vendor', 'angular', 'angular.js'), 'utf8'));
const angular = window.angular;
angular.module('syncthing.core', []);
window.eval(fs.readFileSync(path.join(desuq, 'home.js'), 'utf8'));

// --- the world the stub answers from -------------------------------------

const world = {
    devices: [],
    folders: [],
    connections: {},
    status: {},
    completion: {},
    errors: []
};

angular.module('syncthing.core')
    .factory('$http', function ($q) {
        function reply(data) { return $q.when({ data: data }); }

        const http = function () { return $q.when({ data: {} }); };
        http.get = function (url, config) {
            // $templateRequest passes `cache: $templateCache` to $http and does
            // not consult the cache itself, so a stub that ignores the option
            // sends every template off to the REST API.
            if (config && config.cache && config.cache.get(url) !== undefined) {
                return $q.when({ data: config.cache.get(url) });
            }
            if (url === 'rest/system/status') {
                return reply({ myID: MY_ID });
            }
            if (url === 'rest/config') {
                return reply({
                    devices: [{ deviceID: MY_ID, name: 'this computer' }].concat(world.devices),
                    folders: world.folders
                });
            }
            if (url === 'rest/system/connections') {
                return reply({ connections: world.connections });
            }
            if (url === 'rest/system/error') {
                return reply({ errors: world.errors });
            }
            if (url.indexOf('rest/db/status?folder=') === 0) {
                const id = decodeURIComponent(url.split('folder=')[1]);
                return reply(world.status[id] || {});
            }
            if (url.indexOf('rest/db/completion?') === 0) {
                const f = decodeURIComponent(url.split('folder=')[1].split('&')[0]);
                const d = decodeURIComponent(url.split('device=')[1]);
                return reply(world.completion[f + ' ' + d] || {});
            }
            throw new Error('unstubbed GET ' + url);
        };
        return http;
    });

const injector = angular.injector(['ng', 'syncthing.core']);
const svc = injector.get('desuqHome');
const $rootScope = injector.get('$rootScope');
const $compile = injector.get('$compile');
const $templateCache = injector.get('$templateCache');

$templateCache.put('syncthing/desuq/homeView.html',
    fs.readFileSync(path.join(desuq, 'homeView.html'), 'utf8'));

function flush() { $rootScope.$digest(); }


// =========================================================================
// The rules
// =========================================================================

// Shorthand for a folder as the headline sees one.
function folder(over) {
    return Object.assign({
        id: 'assets', label: 'Project Assets', paused: false, state: 'idle',
        needBytes: 0, localBytes: 12582912, localFiles: 6, globalFiles: 6,
        failedItems: 0, heldBack: 0, heldBackBytes: 0, people: []
    }, over || {});
}

function person(over) {
    return Object.assign({
        deviceID: YUKI, name: 'Yuki', initials: 'Y', connected: true,
        kind: 'complete', pct: 100, needBytes: 0
    }, over || {});
}

function peer(over) {
    return Object.assign({
        deviceID: YUKI, name: 'Yuki', connected: true, paused: false, verified: false, folders: []
    }, over || {});
}

const H = svc._headline;

console.log('The main screen: the headline rules');

console.log('\n-- everything settled says so, quietly');
{
    const h = H([folder({ people: [person()] })], [peer()], []);
    check('tone is good', h.tone === 'good', h.tone);
    check('says everything is here', /Everything is here/.test(h.text), h.text);
    check('names who else has it', /Yuki/.test(h.detail), h.detail);
}

console.log('\n-- holding files back on purpose is not "everything is here" either');
// Selective sync: 1 of 23 files taken, the other 22 sitting in the index
// waiting to be chosen. The folder is genuinely idle and needTotalItems is 0,
// so every local signal says "done" -- which is how the screen came to report
// "1 file, up to date" about a folder somebody was meant to pick from.
{
    const h = H([folder({
        localFiles: 1, localBytes: 3145728, globalFiles: 23,
        heldBack: 22, heldBackBytes: 30670848, people: [person()]
    })], [peer()], []);
    check('still quiet — choosing is normal, not a problem', h.tone === 'good', h.tone);
    check('does not claim to be complete', !/^Everything is here/.test(h.text), h.text);
    check('says what you chose is here', /Everything you chose is here/.test(h.text), h.text);
    check('counts what is still available', /22 more files are available/.test(h.detail), h.detail);
    check('gives the size of it', /29 MiB|30 MiB/.test(h.detail), h.detail);
    check('names the way to get them', /Choose files/.test(h.detail), h.detail);
}

console.log('\n-- a share nobody accepted is not "everything is here"');
// This is the bug the first version of this screen shipped with: locally idle,
// nothing needed, so it reported success about files that had reached nobody.
{
    const h = H([folder({ people: [person({ kind: 'notaccepted', pct: 0 })] })], [peer()], []);
    check('tone is attention, not good', h.tone === 'attention', h.tone);
    check('says who has not accepted', /Yuki/.test(h.text) && /not accepted/.test(h.text), h.text);
    check('does not claim everything is here', !/Everything is here/.test(h.text), h.text);
}

console.log('\n-- a peer still catching up is reported, but quietly');
{
    const h = H([folder({ people: [person({ kind: 'behind', pct: 40, needBytes: 6291456 })] })],
        [peer()], []);
    check('tone is busy', h.tone === 'busy', h.tone);
    check('says it is sending', /Sending/.test(h.text), h.text);
    check('names the person', /Yuki/.test(h.text), h.text);
}

console.log('\n-- incoming files');
{
    const h = H([folder({ state: 'syncing', needBytes: 1048576 })], [peer()], []);
    check('tone is busy', h.tone === 'busy', h.tone);
    check('says it is getting them', /Getting/.test(h.text), h.text);
}
{
    const h = H([folder({ state: 'outofsync', needBytes: 1048576 })],
        [peer({ connected: false })], []);
    check('nobody online says waiting', /Waiting for Yuki/.test(h.text), h.text);
    check('and stays quiet about it', h.tone === 'busy', h.tone);
}

console.log('\n-- things that are actually wrong');
{
    const h = H([folder({ state: 'stopped' })], [peer()], []);
    check('a stopped folder is a problem', h.tone === 'problem', h.tone);
    check('names the folder', /Project Assets/.test(h.text), h.text);
}
{
    const h = H([folder({ state: 'faileditems', failedItems: 3 })], [peer()], []);
    check('failed files are a problem', h.tone === 'problem', h.tone);
    check('counts them', /3 files could not be saved/.test(h.text), h.text);
}
{
    const h = H([folder()], [peer()], ['disk full']);
    check('a system error outranks everything', h.tone === 'problem', h.tone);
}
{
    const h = H([folder({ state: 'localadditions' })], [peer()], []);
    check('receive-only local changes need a decision', h.tone === 'attention', h.tone);
    check('says they are only here', /only on this computer/.test(h.text), h.text);
}

console.log('\n-- the empty and idle shapes');
{
    check('no folders at all', H([], [], []).tone === 'attention');
    check('all paused', /paused/.test(H([folder({ paused: true })], [peer()], []).text));
    check('shared with nobody',
        /not shared with anybody/.test(H([folder({ state: 'unshared' })], [], []).text));
}

console.log('\n-- green is never used, and calm never shouts');
// The two rules the layout enforces, asserted as rules rather than as CSS.
{
    const tones = [
        H([folder({ people: [person()] })], [peer()], []).tone,
        H([folder({ state: 'syncing', needBytes: 10 })], [peer()], []).tone
    ];
    check('settled and working are good/busy only',
        tones.every(t => t === 'good' || t === 'busy'), tones.join(','));
}


// =========================================================================
// Who has what
// =========================================================================

console.log('\nThe main screen: who has what');

console.log('\n-- a share that was never accepted is not 0%');
{
    const s = svc._shareOf(peer(), { completion: 0, remoteState: 'notSharing' });
    check('kind is notaccepted', s.kind === 'notaccepted', s.kind);
    const b = svc._shareOf(peer(), { completion: 0, remoteState: 'valid' });
    check('a real 0% is behind', b.kind === 'behind', b.kind);
}

console.log('\n-- initials distinguish two people with the same first name');
{
    check('two words give two letters', svc._initials('Yuki Tanaka') === 'YT', svc._initials('Yuki Tanaka'));
    check('one word gives one', svc._initials('Yuki') === 'Y', svc._initials('Yuki'));
    check('nothing gives a placeholder', svc._initials('') === '?', svc._initials(''));
}

console.log('\n-- a person\'s card says what is true of them, not what the config says');
// The first version read "You share Project Assets with them" straight off the
// folder's device list, so it could sit directly under a banner reading "Yuki
// has not accepted Project Assets yet". Offering and sharing are different
// things, and the difference is why somebody is looking at this screen.
{
    const P = svc._peerNote;
    check('nothing shared', /No folders shared yet/.test(P([])), P([]));
    check('offered but not accepted is not "you share"',
        /Offered Project Assets — not accepted yet/.test(P([{ label: 'Project Assets', kind: 'notaccepted' }])),
        P([{ label: 'Project Assets', kind: 'notaccepted' }]));
    check('and never claims sharing in that state',
        !/You share|Has /.test(P([{ label: 'Project Assets', kind: 'notaccepted' }])));
    check('accepted and complete says they have it',
        /Has Project Assets/.test(P([{ label: 'Project Assets', kind: 'complete' }])));
    check('still receiving says so',
        /Receiving Project Assets/.test(P([{ label: 'Project Assets', kind: 'behind' }])));
    check('not accepted outranks complete on another folder',
        /Offered Textures/.test(P([
            { label: 'Project Assets', kind: 'complete' },
            { label: 'Textures', kind: 'notaccepted' }
        ])));
}

console.log('\n-- the note under the strip says what the circles mean');
{
    const N = svc._peopleNote;
    check('nobody', /Not shared with anybody/.test(N([])), N([]));
    check('all complete, one person',
        /Yuki has the same files as you/.test(N([person()])), N([person()]));
    check('all complete, several',
        /Everyone has the same files/.test(N([person(), person({ name: 'Ana' })])));
    check('one behind names them and the amount',
        /Yuki is still catching up/.test(N([person({ kind: 'behind', pct: 40, needBytes: 6291456 })])));
    check('not accepted outranks behind',
        /not accepted/.test(N([
            person({ kind: 'behind', pct: 40 }),
            person({ name: 'Ana', kind: 'notaccepted' })
        ])));
    // "has the same files as you" is a claim about both sides, and selective
    // sync makes it false: they have the folder, you have the part you picked.
    check('never claims a match while holding files back',
        /whole folder/.test(N([person()], 22)) && !/same files as you/.test(N([person()], 22)),
        N([person()], 22));
}


// =========================================================================
// The render
// =========================================================================

console.log('\nThe main screen: the render');

world.devices = [
    { deviceID: YUKI, name: 'Yuki' },
    { deviceID: ANA, name: 'Ana Ruiz' }
];
world.folders = [{
    id: 'assets', label: 'Project Assets', path: 'D:\\Assets', type: 'sendreceive',
    devices: [{ deviceID: MY_ID }, { deviceID: YUKI }, { deviceID: ANA }]
}];
world.connections = { [YUKI]: { connected: true, clientVersion: 'v2' } };
world.status = { assets: { state: 'idle', localBytes: 12582912, localFiles: 6, globalBytes: 12582912 } };
world.completion = {
    ['assets ' + YUKI]: { completion: 45, remoteState: 'valid', needBytes: 6291456 },
    ['assets ' + ANA]: { completion: 100, remoteState: 'valid', needBytes: 0 }
};

const scope = $rootScope.$new();
// The template calls the controller's own maps and actions by name; on a real
// page it is a child of syncthingController. Only the maps are read during a
// render, so those are what the harness has to supply.
scope.folders = { assets: world.folders[0] };
scope.devices = { [YUKI]: world.devices[0], [ANA]: world.devices[1] };

const el = $compile('<desuq-home></desuq-home>')(scope);
flush();
flush();

const text = () => el[0].textContent.replace(/\s+/g, ' ').trim();
const html = () => el[0].innerHTML;

check('the screen rendered at all', /Project Assets/.test(text()), text().slice(0, 90));
check('the headline reached the DOM', /Sending/.test(text()), text().slice(0, 90));
check('the folder facts are there', /6 files/.test(text()));
check('the path is shown', /D:\\Assets/.test(text()));

console.log('\n-- the people strip');
check('one circle per person shared with',
    (html().match(/class="[^"]*desuq-home-who [^"]*"/g) || []).length === 2,
    String((html().match(/class="[^"]*desuq-home-who [^"]*"/g) || []).length));
check('the one at 45% is marked behind', /who-behind/.test(html()));
check('the one at 100% is marked complete', /who-complete/.test(html()));
check('the connected one gets a presence badge', /desuq-home-who-badge/.test(html()));
check('the note explains the circles', /catching up/.test(text()), text().slice(0, 120));

console.log('\n-- calm stays quiet');
// The weight rule is carried entirely by the tone class -- is-good and is-busy
// get no border, no tint and no raised surface, is-attention and is-problem do
// -- so the class on that one element is the thing worth asserting. Reading it
// off the element rather than regexing innerHTML also keeps this honest: the
// ng-class attribute itself mentions every tone by name.
const headline = () => el[0].querySelector('.desuq-home-headline').className;

check('a busy headline is not a banner',
    /is-busy/.test(headline()) && !/is-attention|is-problem/.test(headline()),
    headline());

console.log('\n-- and turns loud when it has to');
world.completion['assets ' + YUKI] = { completion: 0, remoteState: 'notSharing', needBytes: 0 };
svc.refresh();
flush();
flush();
check('an unaccepted share becomes a banner', /is-attention/.test(headline()), headline());
check('and says so in words', /not accepted/.test(text()), text().slice(0, 120));

console.log('\n-- no green anywhere');
// The palette rule, asserted where it can actually regress: the stylesheet.
{
    const css = fs.readFileSync(path.join(desuq, 'home.css'), 'utf8');
    check('home.css references no success token', !/--v-success/.test(css));
}

// The screen polls on $timeout for as long as it is on the page, and that is a
// real timer on the jsdom window which will hold node open forever. Destroying
// the scope is what the directive listens for, so this is also the assertion
// that navigating away stops the polling.
scope.$destroy();
window.close();

console.log('');
if (failures) {
    console.log(failures + ' check(s) failed');
    process.exit(1);
}
console.log('all checks passed');
