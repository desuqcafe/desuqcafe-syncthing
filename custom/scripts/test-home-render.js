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
// home.js injects desuqHistory: the two screens are siblings that share open
// state through the service, so the injector cannot build one without the
// other.
window.eval(fs.readFileSync(path.join(desuq, 'history.js'), 'utf8'));

// --- the world the stub answers from -------------------------------------

const world = {
    devices: [],
    folders: [],
    connections: {},
    status: {},
    completion: {},
    reclaimable: {},
    conflicts: { total: 0, folders: [] },
    // An installed copy with its tray in place, which is the ordinary case.
    tray: { expected: true, present: true },
    // Nobody working on anything, and no claims files in any folder.
    claims: { claims: [], folders: [] },
    // What each peer is choosing not to keep: nothing, unless a test says so.
    peerHeld: { files: 0, bytes: 0 },
    // folder id -> /rest/db/delivery answer
    delivery: {},
    deliveryAsked: [],
    // /rest/db/hub: this computer is an edge, and Ana reaches it only
    // through Yuki.
    hub: { folders: [{ folder: 'assets', label: 'Project Assets', through: [],
        via: [{ device: ANA, name: 'Ana', connected: false, via: [{ device: YUKI, name: 'Yuki', connected: true }] }] }] },
    posted: [],
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
            if (url === 'rest/system/tray') {
                return reply(world.tray);
            }
            if (url === 'rest/db/hub') {
                return reply(world.hub);
            }
            if (url === 'rest/db/delivery') {
                world.deliveryAsked.push(config.params.folder);
                return reply(world.delivery[config.params.folder] || { folder: config.params.folder, peers: [] });
            }
            if (url.indexOf('rest/db/peerheldback') === 0) {
                return reply(world.peerHeld);
            }
            if (url === 'rest/folder/claims') {
                return reply(world.claims);
            }
            if (url.indexOf('rest/db/status?folder=') === 0) {
                const id = decodeURIComponent(url.split('folder=')[1]);
                return reply(world.status[id] || {});
            }
            if (url.indexOf('rest/folder/conflicts') === 0) {
                return reply(world.conflicts);
            }
            if (url.indexOf('rest/db/reclaimable') === 0) {
                const id = decodeURIComponent(url.split('folder=')[1] || '');
                return reply(world.reclaimable[id] || { bytes: 0, files: 0, peers: [] });
            }
            if (url.indexOf('rest/db/completion?') === 0) {
                const f = decodeURIComponent(url.split('folder=')[1].split('&')[0]);
                const d = decodeURIComponent(url.split('device=')[1]);
                return reply(world.completion[f + ' ' + d] || {});
            }
            throw new Error('unstubbed GET ' + url);
        };
        // Every POST is recorded, so a test can assert what a button sent.
        http.post = function (url, body) {
            world.posted.push({ url: url, body: body });
            if (url === 'rest/folder/claim') {
                // The server answers with the folder's claims afterwards.
                world.claims.claims = world.claims.claims.filter(c =>
                    !(c.folder === body.folder && c.path === body.path && c.mine && body.release));
                return reply({
                    claims: world.claims.claims.filter(c => c.folder === body.folder),
                    folders: world.claims.folders.filter(f => f.folder === body.folder)
                });
            }
            if (url === 'rest/db/hub/connect') {
                return reply({ device: body.device, name: 'Ana' });
            }
            throw new Error('unstubbed POST ' + url);
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
// Nothing chosen at all: closing the picker holds everything back, and Resume
// then runs a folder that fetches nothing. Seen on a live pair, where it read
// "Everything you chose is here" over zero files nobody had chosen.
{
    const h = H([folder({
        localFiles: 0, localBytes: 0, globalFiles: 9,
        heldBack: 9, heldBackBytes: 14680064, people: [person()]
    })], [peer()], []);
    check('nothing picked asks for a pick', h.tone === 'attention', h.tone);
    check('and says nothing is picked', /Nothing has been picked yet/.test(h.text), h.text);
    check('never "everything you chose"', !/Everything you chose/.test(h.text));
    check('counts what is waiting', /9 files are available \(14 MiB\) and none are/.test(h.detail), h.detail);
    const one = H([folder({ localFiles: 0, localBytes: 0, globalFiles: 1, heldBack: 1, heldBackBytes: 1024 })], [peer()], []);
    check('one file reads as one', /1 file is available \(1 KiB\) and it is not/.test(one.detail), one.detail);
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

console.log('\n-- Windows Security took the tray');
// DEPLOYMENT-3D-TEAM.md section 20. Syncthing is still running, so every other
// fact on the screen is true -- until the restart, after which nothing starts.
{
    const gone = { expected: true, present: false };
    const h = H([folder({ people: [person()] })], [peer()], [], gone);
    check('a missing tray is a problem even when all is in step', h.tone === 'problem', h.tone);
    check('says what it will cost', /stop after you restart/.test(h.text), h.text);
    check('says it is a false alarm', /false alarm/.test(h.detail));
    check('and how to put it back', /Protection history/.test(h.detail) && /Restore/.test(h.detail));
    check('it outranks a stopped folder',
        /Windows Security/.test(H([folder({ state: 'stopped' })], [peer()], [], gone).text));
    check('a system error still outranks it',
        /problem needs/.test(H([folder()], [peer()], ['disk full'], gone).text));
}
{
    // A development build answers expected:false; it must never raise this.
    check('a build that is not installed is never alarmed about',
        H([folder({ people: [person()] })], [peer()], [], { expected: false, present: false }).tone === 'good');
    check('nor is a tray that is there',
        H([folder({ people: [person()] })], [peer()], [], { expected: true, present: true }).tone === 'good');
    check('nor an answer that has not arrived yet',
        H([folder({ people: [person()] })], [peer()], [], null).tone === 'good');
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
    // What the server actually answers when a folder is shared with somebody
    // already connected: their last cluster config predates it, so 'unknown',
    // with the whole folder as need. Seen on a live pair, where it read as
    // "catching up -- 12 MiB to go" about a share nobody had accepted.
    const u = svc._shareOf(peer(), { completion: 0, remoteState: 'unknown', needBytes: 12582912 });
    check('unknown from a connected peer is notaccepted', u.kind === 'notaccepted', u.kind);
    const d = svc._shareOf(peer({ connected: false }), { completion: 0, remoteState: 'unknown' });
    check('but from a disconnected one it claims nothing', d.kind !== 'notaccepted', d.kind);
}

console.log('\n-- a peer who keeps part of a folder is not "the same"');
// Completion says 100% for a peer who took one texture out of six: an ignored
// file is not needed. Their own index says otherwise. Seen on a live pair:
// "Yuki Laptop has the same files as you" about a machine holding one file.
{
    const held = { files: 5, bytes: 10485760 };
    const s = svc._shareOf(peer(), { completion: 100, remoteState: 'valid' }, held);
    check('complete by completion, holding back by index, is partial', s.kind === 'partial', s.kind);
    check('without the index, still complete', svc._shareOf(peer(), { completion: 100, remoteState: 'valid' }).kind === 'complete');
    check('nothing held back is complete', svc._shareOf(peer(), { completion: 100, remoteState: 'valid' }, { files: 0 }).kind === 'complete');
    check('a peer still pulling is behind, whatever they hold back',
        svc._shareOf(peer(), { completion: 40, remoteState: 'valid' }, held).kind === 'behind');

    const N = svc._peopleNote;
    const p = person({ kind: 'partial', heldFiles: 5, heldBytes: 10485760 });
    check('the note says they keep part of it, and how much',
        /Yuki keeps only part of it — 5 files are not on their computer \(10 MiB\)/.test(N([p], 0, 6)), N([p], 0, 6));
    check('and never "the same files"', !/same files/.test(N([p], 0, 6)));
    check('beside someone who has it all, both are said',
        /keeps only part of it/.test(N([p, person({ name: 'Ana' })], 0, 6)) &&
        /Ana has the same files as you/.test(N([p, person({ name: 'Ana' })], 0, 6)),
        N([p, person({ name: 'Ana' })], 0, 6));
    check('both holding back: both directions are said',
        /Yuki keeps only part of it/.test(N([p], 3, 2)) && /You have chosen part of it/.test(N([p], 3, 2)),
        N([p], 3, 2));

    const h = H([folder({ people: [p] })], [peer()], []);
    check('the headline stops saying "has the same"', !/has the same/.test(h.detail), h.detail);
    check('and says who keeps part of it', /Yuki keeps only part of it/.test(h.detail), h.detail);
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
    check('says part of it when part was picked', /chosen part of it/.test(N([person()], 22, 1)), N([person()], 22, 1));
    check('and not when nothing was',
        /not picked anything/.test(N([person()], 9, 0)) && !/chosen part/.test(N([person()], 9, 0)),
        N([person()], 9, 0));
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
check('delivery is asked for the folder somebody is behind on',
    world.deliveryAsked.indexOf('assets') !== -1, JSON.stringify(world.deliveryAsked));

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

console.log('\n-- two copies of the same file');
// The count is on a one-minute probe of its own, so the poll that follows a
// refresh() is not enough on its own -- markConflictsStale is what the history
// screen's broadcast reaches, and what this asserts is reachable.
world.conflicts = {
    total: 2,
    folders: [{ folder: 'assets', label: 'Project Assets', count: 2, bytes: 2048, versioning: true, rows: [] }]
};
svc.markConflictsStale();
svc.refresh();
flush();
flush();
check('the card says both copies were kept', /changed in two places at once/.test(text()), text().slice(0, 160));
check('and names the number', /2 files were/.test(text()));
check('with a way through to the choice', /Choose between them/.test(text()));

// A count that has gone to zero must take the row with it: this row is a
// decision waiting, and a decision that has been made is not one.
world.conflicts = { total: 0, folders: [] };
svc.markConflictsStale();
svc.refresh();
flush();
flush();
check('the row goes when the conflicts do', !/changed in two places at once/.test(text()));

console.log('\n-- somebody reached only through somebody else');
svc.refresh();
flush();
flush();
check('the card says who is reached only through whom',
    /You get Ana's changes only through Yuki's computer/.test(text()), text().slice(0, 200));
{
    const btn = Array.prototype.find.call(el[0].querySelectorAll('.desuq-home-hub button'),
        b => /Connect directly/.test(b.textContent));
    check('with a button to connect directly', !!btn);
    if (btn) {
        btn.click();
        flush();
        const last = world.posted[world.posted.length - 1] || {};
        check('which asks the server to add that person to that folder',
            last.url === 'rest/db/hub/connect' && last.body.folder === 'assets' && last.body.device === ANA,
            JSON.stringify(last));
        check('and says what happens next', /They need to accept on their computer/.test(text()));
        check('and the button goes', !Array.prototype.some.call(el[0].querySelectorAll('.desuq-home-hub button'),
            b => /Connect directly/.test(b.textContent)));
    }
}

console.log('\n-- a folder that has stopped');
// This is the state one of the author's own folders has been in since 24
// August: "Stopped", and nothing to press. All three ways out are named, and
// the dangerous one is a server call with its own rails (api_repair.go).
world.status = { assets: { state: 'error', error: 'folder path missing' } };
svc.refresh();
flush();
flush();
check('the card says it has stopped', /This folder has stopped/.test(text()), text().slice(0, 200));
check('and repeats what Syncthing said', /folder path missing/.test(text()));
check('the drive is the first thing suggested', /plug it in/.test(text()));
check('and there is a way to point it somewhere else', /Find it/.test(text()));
check('and a way to set it up again', /Set it up again/.test(text()));

console.log('\n-- a tray that has been quarantined');
// On a one-minute probe, like conflicts. The first answer in this harness was
// "present", so this also asserts the probe is asked again rather than once.
// Two refreshes: the probe's answer lands after the headline is computed, so
// the banner is the tick after -- 2.5 seconds on a real page.
world.status = { assets: { state: 'idle', localBytes: 12582912, localFiles: 6, globalBytes: 12582912 } };
world.tray = { expected: true, present: false };
{
    // home.js runs inside the jsdom window, so it is *that* Date which has
    // to move, not node's.
    const realNow = window.Date.now;
    window.Date.now = () => realNow() + 61000;
    svc.refresh();
    flush();
    flush();
    svc.refresh();
    flush();
    flush();
    window.Date.now = realNow;
}
check('the headline turns into the banner', /is-problem/.test(headline()), headline());
check('and says Windows Security did it', /Windows Security removed/.test(text()), text().slice(0, 160));

console.log('\n-- who is working on what');
// lib/api/api_claims.go. The claims files are in the folder, so Syncthing's
// counts include them; the card must not.
world.tray = { expected: true, present: true };
world.status = { assets: { state: 'idle', localBytes: 12582912 + 300, localFiles: 8, globalBytes: 12582912 + 300, globalFiles: 8 } };
world.claims = {
    claims: [
        { folder: 'assets', label: 'Project Assets', path: 'mine.blend', device: MY_ID, name: 'You', mine: true, since: new Date().toISOString(), stale: false },
        { folder: 'assets', label: 'Project Assets', path: 'Scenes/cabin.blend', device: YUKI, name: 'Yuki', mine: false, since: new Date().toISOString(), stale: false }
    ],
    folders: [{ folder: 'assets', canClaim: true, files: 2, bytes: 300 }]
};
{
    const realNow = window.Date.now;
    window.Date.now = () => realNow() + 200000;
    svc.refresh();
    flush();
    flush();
    svc.refresh();
    flush();
    flush();
    window.Date.now = realNow;
}
{
    const rows = [...el[0].querySelectorAll('.desuq-home-claim')];
    check('each claim is a row on the card', rows.length === 2, String(rows.length));
    check('somebody else\'s comes first', rows[0] && /Yuki is working on/.test(rows[0].textContent),
        rows[0] && rows[0].textContent.replace(/\s+/g, ' '));
    check('and names the file', rows[0] && /Scenes\/cabin\.blend/.test(rows[0].textContent));
    check('mine says so, quietly', rows[1] && /claim-mine/.test(rows[1].className) &&
        /You are working on/.test(rows[1].textContent));
    check('only mine has a Done button',
        !rows[0].querySelector('button') && !!(rows[1] && rows[1].querySelector('button')));
    check('the claims files are not counted as work', /6 files/.test(text()) && !/8 files/.test(text()),
        text().slice(0, 200));
    check('there is a way to find out how to mark a file', /Working on a file\?/.test(text()));

    rows[1].querySelector('button').click();
    flush();
    flush();
    const last = world.posted[world.posted.length - 1] || {};
    check('Done asks the server to release that file, and only that',
        last.url === 'rest/folder/claim' && last.body.folder === 'assets' &&
        last.body.path === 'mine.blend' && last.body.release === true,
        JSON.stringify(last));
    flush();
    const after = [...el[0].querySelectorAll('.desuq-home-claim')];
    check('and the row goes', after.length === 1 && /Yuki/.test(after[0].textContent), String(after.length));
}
{
    // A receive-only copy cannot send a mark, so it is not offered one.
    world.claims.folders = [{ folder: 'assets', canClaim: false, reason: 'only receives', files: 1, bytes: 150 }];
    const realNow = window.Date.now;
    window.Date.now = () => realNow() + 400000;
    svc.refresh();
    flush();
    flush();
    svc.refresh();
    flush();
    flush();
    window.Date.now = realNow;
    check('a folder that cannot send a mark does not offer to explain how', !/Working on a file\?/.test(text()));
    check('but still shows the marks that arrive', /Yuki is working on/.test(text()));
}
{
    const S = svc.claimSince;
    const now = new Date();
    check('today reads as a time', /^since \d\d:\d\d$/.test(S(now.toISOString(), false)), S(now.toISOString(), false));
    const old = new Date(now.getTime() - 4 * 86400000).toISOString();
    check('a stale one says how old and why it matters', /4 days, may have been forgotten/.test(S(old, true)), S(old, true));
    check('nonsense reads as nothing', S('not a date', false) === '');
}

console.log('\n-- a peer whose copy only receives, and who changed things in it');
// Verified on a pair, 2026-09-26: one edit and one deletion on a receive-only
// B read on A as 60%, two items needed, remote state valid -- and the screen
// said "Sending 2 MiB to Yuki. Theirs is catching up" about a copy that will
// never catch up on its own.
{
    const s = svc._shareOf(peer(), { completion: 60, remoteState: 'valid', needBytes: 2097152 },
        { files: 0, bytes: 0, changed: 2 });
    check('changed there is its own kind, not behind', s.kind === 'changedthere', s.kind);
    check('and carries the count', s.changedFiles === 2, s.changedFiles);
    check('paused still outranks it',
        svc._shareOf(peer({ paused: true }), { completion: 60, remoteState: 'valid' }, { changed: 2 }).kind === 'paused');

    const p = person({ kind: 'changedthere', changedFiles: 2, pct: 60, needBytes: 2097152 });
    const N = svc._peopleNote;
    check('the note says the changes stay with them',
        /Yuki changed 2 files on their own computer\. Their copy only receives/.test(N([p], 0, 6)), N([p], 0, 6));
    check('and never that they are catching up', !/catching up/.test(N([p], 0, 6)));
    check('the card note says so too',
        /Has changes in Project Assets that stay on their computer/.test(
            svc._peerNote([{ label: 'Project Assets', kind: 'changedthere' }])));

    const h = H([folder({ people: [p] })], [peer()], []);
    check('the headline does not claim to be sending to them', !/Sending/.test(h.text), h.text);
    check('and says they have changes of their own', /changes that stay on their computer/.test(h.detail), h.detail);
}

console.log('\n-- behind and not connected is away, not catching up');
// Completion reads the same whether the peer is here or not. The screen said
// "Sending 12 MiB to Yuki -- theirs is catching up" about a computer that was
// switched off. /rest/db/delivery says which of it is yours.
{
    const dv = { yours: { files: 3, bytes: 900 }, names: ['Scenes\\cabin.blend', 'tree.blend', 'rock.blend'] };
    const a = svc._shareOf(peer({ connected: false }), { completion: 40, remoteState: 'valid', needBytes: 12582912 }, null, dv);
    check('a disconnected peer at 40% is away', a.kind === 'away', a.kind);
    check('carrying your files, by their last name', a.yoursFiles === 3 && a.yoursNames[0] === 'cabin.blend', JSON.stringify(a.yoursNames));
    const b = svc._shareOf(peer(), { completion: 40, remoteState: 'valid' }, null, dv);
    check('the same peer connected is behind', b.kind === 'behind', b.kind);
    const c = svc._shareOf(peer(), { completion: 100, remoteState: 'valid' }, null, dv);
    check('a stale delivery answer is ignored once they are complete', c.kind === 'complete' && c.yoursFiles === 0, c.yoursFiles);

    const Y = svc._yoursPhrase;
    check('one file is named', Y({ yoursFiles: 1, yoursNames: ['cabin.blend'] }) === 'cabin.blend');
    check('two are both named', Y({ yoursFiles: 2, yoursNames: ['cabin.blend', 'tree.blend'] }) === 'cabin.blend and tree.blend');
    check('more are counted', Y({ yoursFiles: 7, yoursNames: ['cabin.blend', 'x', 'y', 'z', 'w'] }) === 'cabin.blend and 6 more');

    const N = svc._peopleNote;
    const away = person({ kind: 'away', connected: false, pct: 40, needBytes: 12582912, yoursFiles: 1, yoursNames: ['cabin.blend'] });
    const note = N([away], 0, 6);
    check('the note says your latest has not reached them', /Yuki does not have your latest cabin\.blend yet/.test(note), note);
    check('and why, and what happens next', /not connected\. It goes when they are back/.test(note), note);
    check('and never that they are catching up', !/catching up/.test(note), note);
    const theirs = N([person({ kind: 'away', connected: false, needBytes: 1048576, yoursFiles: 0 })], 0, 6);
    check('away with nothing of yours says what waits for them', /Yuki is not connected\. 1(\.0)? MiB of changes will reach them/.test(theirs), theirs);

    const sending = N([person({ kind: 'behind', pct: 40, needBytes: 900, yoursFiles: 1, yoursNames: ['cabin.blend'] })], 0, 6);
    check('connected and missing yours: sending, by name', sending === 'Sending cabin.blend to Yuki.', sending);

    const h = H([folder({ people: [away] })], [peer({ connected: false })], []);
    check('the headline does not claim to be sending to somebody away', !/Sending/.test(h.text), h.text);
    check('it is still everything is here', /Everything is here/.test(h.text), h.text);
    check('and the detail says your latest has not reached them', /Yuki does not have your latest yet — not connected/.test(h.detail), h.detail);

    const hs = H([folder({ people: [person({ kind: 'behind', pct: 40, needBytes: 900, yoursFiles: 1, yoursNames: ['cabin.blend'] })] })], [peer()], []);
    check('sending headline names what is on its way', /On its way: cabin\.blend to Yuki/.test(hs.detail), hs.detail);

    check('the card note for away', /Not connected — Project Assets will catch up/.test(
        svc._peerNote([{ label: 'Project Assets', kind: 'away' }])));
}

console.log('\n-- only syncing through one computer');
{
    const HN = svc._hubNotes;
    check('nothing known says nothing', HN(undefined).through === '' && HN(undefined).via.length === 0);
    const mid = HN({ through: [{ a: { name: 'Kai' }, b: { name: 'Mia' } }], via: [] });
    check('the middle names the pair and the consequence',
        /^Kai and Mia only sync with each other through this computer\. When it is off/.test(mid.through), mid.through);
    check('and where the fix is', /Connect directly/.test(mid.through));
    const edge = HN({ through: [], via: [{ device: 'D', name: 'Mia', via: [{ name: 'Alex' }] }] });
    check('the edge names who and through whom',
        edge.via.length === 1 && /^You get Mia's changes only through Alex's computer\./.test(edge.via[0].text),
        edge.via[0] && edge.via[0].text);
    const three = HN({ through: [{ a: { name: 'A' }, b: { name: 'B' } }, { a: { name: 'A' }, b: { name: 'C' } }], via: [] });
    check('several pairs are one sentence, each name once', /^A, B,? and C only sync/.test(three.through), three.through);
}

console.log('\n-- a local-only folder warns what switching it would do');
{
    const h = H([folder({ state: 'localadditions' })], [peer()], []);
    check('the headline names the trap', /deletions included/.test(h.detail), h.detail);
}

console.log('\n-- one of your marks that has not reached somebody');
{
    const U = svc.claimUnseen;
    check('reached everybody says nothing', U([]) === '' && U(undefined) === '');
    check('on its way to a connected peer says nothing',
        U([{ name: 'Yuki', state: 'sending' }]) === '', U([{ name: 'Yuki', state: 'sending' }]));
    check('offline is said, with what happens next',
        /Yuki is offline and will see this when they reconnect/.test(U([{ name: 'Yuki', state: 'offline' }])));
    check('a copy that cannot take marks is said',
        /Ana cannot see marks until their copy is updated/.test(U([{ name: 'Ana', state: 'heldBack' }])));
}

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
