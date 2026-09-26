// Drives the history screen through real Angular against canned REST, and
// asserts the two pieces of logic that would fail quietly.
//
//     npm install jsdom      (once, anywhere on the path)
//     node custom/scripts/test-history-render.js
//
// No Syncthing needed, for the same reason test-home-render.js needs none:
// the screen reads its whole state over REST, so $http is the only thing that
// has to be stubbed.
//
// WHAT IS ACTUALLY AT RISK HERE
//
// Two things, and neither of them is layout.
//
// The RESTART GUARD. /rest/events/disk numbers its events from 1 and its
// buffer is memory only, so a daemon restart hands out ids that are newer in
// time and lower in number. A poll that keeps its old `since` then appends
// them out of order and never advances again -- the feed silently freezes and
// looks exactly like a quiet afternoon. That is the single worst failure this
// screen can have, because there is nothing on screen to notice.
//
// The RUN COLLAPSE. The first scan of a folder emits one event per existing
// file, all with one timestamp. Without folding, opening this screen after a
// restart is three hundred identical rows. With it folded wrongly -- across
// people, or across folders -- the screen attributes somebody's work to
// somebody else, which is worse than the three hundred rows.
//
// The wording is asserted too, in one specific: the feed must never say
// "added". action is only ever "modified" or "deleted" (lib/model/folder.go),
// so a file that has just been created arrives as modified and this screen
// cannot tell the two apart. Saying "added" would be a claim the data does not
// support.

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
const KAI = 'X7Z653J-YKPD7JT-6NVMY7Z-6GK5LC7-I6YPKLH-AG435RY-UOIUUXS-54EPPQO';
const MY_SHORT = 'AAAAAAA';
const KAI_SHORT = 'X7Z653J';

const dom = new JSDOM(
    '<!DOCTYPE html><html><body><div id="host"></div></body></html>',
    { runScripts: 'outside-only', pretendToBeVisual: true, url: 'http://127.0.0.1:8384/' }
);

const { window } = dom;
global.window = window;
global.document = window.document;
global.navigator = window.navigator;

window.urlbase = 'rest';

window.eval(fs.readFileSync(
    path.join(REPO, 'gui', 'default', 'vendor', 'angular', 'angular.js'), 'utf8'));
const angular = window.angular;
angular.module('syncthing.core', []);
window.eval(fs.readFileSync(path.join(desuq, 'history.js'), 'utf8'));

// The file-scope helpers, reached through the same window the module was
// evaluated in. history.js keeps them dependency-free precisely so this works.
const pretty = window.pretty;
const dayTitle = window.dayTitle;
const desuqBytes = window.desuqBytes;

// --- the world the stub answers from -------------------------------------

const world = {
    diskEvents: [],
    sinceEvents: {},
    // The newest id in the buffer, for the restart check; null is empty.
    newest: null,
    newestAsked: 0,
    archive: {},
    fileVersions: {},
    conflicts: { total: 0, folders: [] },
    // What POST /rest/folder/conflict answers with. A function so a case can
    // reject: the refusal path is the interesting one.
    resolve: null,
    // What POST /rest/folder/pin answers with, when a case wants a refusal.
    pin: null,
    posted: []
};

angular.module('syncthing.core')
    .factory('$http', function ($q) {
        function reply(data) { return $q.when({ data: data }); }

        const http = function () { return $q.when({ data: {} }); };
        http.get = function (url, config) {
            // $templateRequest passes `cache: $templateCache` to $http and does
            // not consult the cache itself.
            if (config && config.cache && config.cache.get(url) !== undefined) {
                return $q.when({ data: config.cache.get(url) });
            }
            if (url === 'rest/system/status') {
                return reply({ myID: MY_ID });
            }
            if (url === 'rest/config') {
                return reply({
                    devices: [
                        { deviceID: MY_ID, name: 'this computer' },
                        { deviceID: KAI, name: 'Kai' }
                    ],
                    folders: [{ id: 'assets', label: 'Shared Art' }]
                });
            }
            if (url === 'rest/events/disk') {
                const p = (config && config.params) || {};
                // "Where has the buffer got to": the newest one only.
                if (p.limit === 1) {
                    world.newestAsked++;
                    return reply(world.newest === null ? [] : [{ id: world.newest }]);
                }
                if (p.since !== undefined) {
                    return reply(world.sinceEvents[p.since] || []);
                }
                return reply(world.diskEvents);
            }
            if (url === 'rest/folder/history') {
                const p = (config && config.params) || {};
                if (p.file) {
                    return reply({
                        folder: p.folder, name: p.file,
                        deleted: false,
                        versions: world.fileVersions[p.file] || []
                    });
                }
                return reply(world.archive[p.folder] ||
                    { versioning: true, files: 0, versions: 0, bytes: 0, total: 0, rows: [] });
            }
            if (url === 'rest/folder/conflicts') {
                return reply(world.conflicts);
            }
            throw new Error('unstubbed GET ' + url);
        };
        http.post = function (url, body) {
            world.posted.push({ url: url, body: body });
            if (url === 'rest/folder/conflict' && world.resolve) {
                return world.resolve(body);
            }
            if (url === 'rest/folder/pin' && world.pin) {
                return world.pin(body);
            }
            return $q.when({ data: {} });
        };
        return http;
    });

const injector = angular.injector(['ng', 'syncthing.core']);
const svc = injector.get('desuqHistory');
const $rootScope = injector.get('$rootScope');
const $compile = injector.get('$compile');
const $templateCache = injector.get('$templateCache');

$templateCache.put('syncthing/desuq/historyView.html',
    fs.readFileSync(path.join(desuq, 'historyView.html'), 'utf8'));

function flush() { $rootScope.$digest(); }

// A disk event as the server sends one.
function ev(id, over) {
    return Object.assign({
        id: id,
        time: '2026-08-26T14:22:00+09:00',
        type: 'RemoteChangeDetected',
        data: {
            folder: 'assets', folderID: 'assets', label: 'Shared Art',
            action: 'modified', type: 'file',
            path: 'refs\\chair_v3.blend',
            modifiedBy: KAI_SHORT
        }
    }, over || {});
}

function at(iso, over) {
    const e = ev(1, over);
    e.time = iso;
    return e;
}


// =========================================================================
// The restart guard
// =========================================================================

console.log('\n-- the restart guard');

{
    // Ordinary case: ids move forward, nothing is thrown away.
    const r = svc._resetIfRestarted([{ id: 4212 }, { id: 4213 }], 4211);
    check('a forward id is not a restart', r.reset === false);
    check('and the buffered events survive', r.events === 3);
}

{
    // The real one: the daemon restarted, so ids begin again at 1 while being
    // newer in time than the 4211 we last saw.
    const r = svc._resetIfRestarted([{ id: 1 }, { id: 2 }], 4211);
    check('an id going backwards is a restart', r.reset === true);
    check('the buffer is dropped', r.events === 0);
    check('and since is reset, not left at the old high water mark', r.lastID === 0);
}

{
    // The boundary. An id equal to `since` is not newer, and treating it as
    // progress is how the feed would stall on a repeat.
    const r = svc._resetIfRestarted([{ id: 4211 }], 4211);
    check('an id equal to since counts as a restart', r.reset === true);
}


// =========================================================================
// The run collapse
// =========================================================================

console.log('\n-- collapsing runs');

// who() resolves a modifiedBy short id through the device list, which open()
// fetches. Collapsing is otherwise pure, so one open is enough to populate it
// for every case below -- and doing it here rather than inside the service
// keeps the fact visible: a row reading "X7Z653J changed 4 files" is exactly
// what this screen looks like before the config has arrived.
svc.open('', 'changes');
flush();
svc.close();

{
    // A scan: many files, one person, one folder, one timestamp.
    const list = [];
    for (let i = 0; i < 40; i++) {
        list.push(at('2026-08-26T14:22:00+09:00', {
            data: {
                folder: 'assets', label: 'Shared Art', action: 'modified',
                type: 'file', path: 'refs\\file' + i + '.png',
                modifiedBy: KAI_SHORT
            }
        }));
    }
    const rows = svc._collapse(list);
    check('forty files in one second are one row', rows.length === 1, 'rows=' + rows.length);
    check('and the row counts all of them', rows[0].count === 40);
    check('naming only the first few', rows[0].names.length === 6);
}

{
    // Two people must never fold together, however close in time.
    const rows = svc._collapse([
        at('2026-08-26T14:22:00+09:00', { data: { folder: 'assets', label: 'A', action: 'modified', type: 'file', path: 'a.png', modifiedBy: KAI_SHORT } }),
        at('2026-08-26T14:22:01+09:00', { data: { folder: 'assets', label: 'A', action: 'modified', type: 'file', path: 'b.png', modifiedBy: MY_SHORT } })
    ]);
    check('two people are two rows', rows.length === 2, 'rows=' + rows.length);
    check('and our own short id reads as You', rows[1].person === 'You', rows[1].person);
    check('while a known peer reads by name', rows[0].person === 'Kai', rows[0].person);
}

{
    // Nor two folders.
    const rows = svc._collapse([
        at('2026-08-26T14:22:00+09:00', { data: { folder: 'assets', label: 'A', action: 'modified', type: 'file', path: 'a.png', modifiedBy: KAI_SHORT } }),
        at('2026-08-26T14:22:01+09:00', { data: { folder: 'other', label: 'B', action: 'modified', type: 'file', path: 'b.png', modifiedBy: KAI_SHORT } })
    ]);
    check('two folders are two rows', rows.length === 2, 'rows=' + rows.length);
}

{
    // Nor a change and a deletion, which is the fold that would let a row read
    // "Kai changed 12 files" about eleven changes and one deletion.
    const rows = svc._collapse([
        at('2026-08-26T14:22:00+09:00', { data: { folder: 'assets', label: 'A', action: 'modified', type: 'file', path: 'a.png', modifiedBy: KAI_SHORT } }),
        at('2026-08-26T14:22:01+09:00', { data: { folder: 'assets', label: 'A', action: 'deleted', type: 'file', path: 'b.png', modifiedBy: KAI_SHORT } })
    ]);
    check('changing and removing are two rows', rows.length === 2, 'rows=' + rows.length);
}

{
    // Past the minute, the same person doing the same thing is a second visit
    // to the folder, not a continuation.
    const rows = svc._collapse([
        at('2026-08-26T14:22:00+09:00', { data: { folder: 'assets', label: 'A', action: 'modified', type: 'file', path: 'a.png', modifiedBy: KAI_SHORT } }),
        at('2026-08-26T14:25:00+09:00', { data: { folder: 'assets', label: 'A', action: 'modified', type: 'file', path: 'b.png', modifiedBy: KAI_SHORT } })
    ]);
    check('three minutes apart is two rows', rows.length === 2, 'rows=' + rows.length);
}


// =========================================================================
// The helpers
// =========================================================================

console.log('\n-- helpers');

check('a windows path yields the file name', pretty('refs\\wood\\chair.blend') === 'chair.blend');
check('a slash path does too', pretty('refs/wood/chair.blend') === 'chair.blend');
check('a bare name is left alone', pretty('chair.blend') === 'chair.blend');
check('nothing does not crash', pretty('') === '');

check('bytes under a kibibyte are bytes', desuqBytes(512) === '512 B', desuqBytes(512));
check('and above it are not', desuqBytes(1536) === '1.5 KiB', desuqBytes(1536));
check('three gigabytes read as gigabytes', desuqBytes(3 * 1024 * 1024 * 1024) === '3 GiB', desuqBytes(3 * 1024 * 1024 * 1024));

{
    const now = new Date();
    check('today is Today', dayTitle(now) === 'Today', dayTitle(now));
    const y = new Date(now.getTime() - 86400000);
    check('yesterday is Yesterday', dayTitle(y) === 'Yesterday', dayTitle(y));
    // Older than a week gets a date rather than a weekday, because "Tuesday"
    // stops being unambiguous at eight days.
    const old = new Date(now.getTime() - 20 * 86400000);
    check('three weeks ago is a date', /\d/.test(dayTitle(old)) && dayTitle(old) !== 'Today', dayTitle(old));
}


// =========================================================================
// The render
// =========================================================================

console.log('\n-- the changes tab');

function render() {
    const el = $compile('<desuq-history></desuq-history>')($rootScope.$new());
    flush();
    return el;
}

{
    world.diskEvents = [
        ev(1, {
            time: '2026-08-26T11:05:00+09:00',
            data: { folder: 'assets', label: 'Shared Art', action: 'modified', type: 'file', path: 'wood_albedo.png', modifiedBy: MY_SHORT }
        }),
        ev(2, {
            time: '2026-08-26T14:22:00+09:00',
            data: { folder: 'assets', label: 'Shared Art', action: 'modified', type: 'file', path: 'refs\\chair_v3.blend', modifiedBy: KAI_SHORT }
        }),
        ev(3, {
            time: '2026-08-26T18:40:00+09:00',
            data: { folder: 'assets', label: 'Shared Art', action: 'deleted', type: 'file', path: 'old_test.blend', modifiedBy: KAI_SHORT }
        })
    ];

    const el = render();
    svc.open('', 'changes');
    flush();
    const text = el.text();

    check('the screen renders once opened', text.indexOf('Recent changes') !== -1);
    check('a peer change names the peer', text.indexOf('Kai') !== -1);
    check('our own change reads as You', text.indexOf('You') !== -1);
    check('a deletion says removed', text.indexOf('removed') !== -1);

    // The wording rule. action is only modified or deleted, so nothing here
    // can honestly claim a file was created.
    check('nothing claims a file was added', !/\badded\b/i.test(text), text.slice(0, 90));

    check('the footer admits the record is partial',
        text.indexOf('not a complete record') !== -1);
}

{
    // A directory event is plumbing and must not become a row.
    world.diskEvents = [
        ev(1, { data: { folder: 'assets', label: 'Shared Art', action: 'modified', type: 'dir', path: 'refs', modifiedBy: KAI_SHORT } })
    ];
    svc.close();
    const el = render();
    svc.open('', 'changes');
    flush();
    const text = el.text();
    check('a directory event is not a row', text.indexOf('Nothing has changed') !== -1, text.slice(0, 80));
}

{
    // The empty state has to name the boundary. An empty list means "since
    // this computer started", and a person who reads it as "nobody has
    // touched anything" is being misled by silence.
    world.diskEvents = [];
    svc.close();
    const el = render();
    svc.open('', 'changes');
    flush();
    const text = el.text();
    check('empty says since this computer started',
        text.indexOf('since this computer started') !== -1);
    check('and points at the tab that survives a restart',
        text.indexOf('Older versions') !== -1);
}

{
    // Marking a file "I'm working on this" writes a claims file. That is not
    // somebody's work and must not appear as a change.
    world.diskEvents = [
        ev(1, { data: { folder: 'assets', label: 'Shared Art', action: 'modified', type: 'file', path: '.desuq-claims\\ABC.json', modifiedBy: KAI_SHORT } })
    ];
    svc.close();
    const el = render();
    svc.open('', 'changes');
    flush();
    check('a claims file changing is not a row', el.text().indexOf('Nothing has changed') !== -1,
        el.text().slice(0, 80));
}

{
    // The restart the guard above cannot see. Asked for events after 4211, a
    // restarted daemon answers NOTHING -- lib/events waits for its counter to
    // pass 4211 first -- so resetIfRestarted never gets an event to look at
    // and the feed silently stops. Found by reading lib/events/events.go
    // Since(); the stub used to hand back ids from 1, which the server never
    // does.
    world.diskEvents = [
        ev(4210, { data: { folder: 'assets', label: 'Shared Art', action: 'modified', type: 'file', path: 'a.blend', modifiedBy: KAI_SHORT } }),
        ev(4211, { data: { folder: 'assets', label: 'Shared Art', action: 'modified', type: 'file', path: 'b.blend', modifiedBy: KAI_SHORT } })
    ];
    world.sinceEvents = {};
    world.newest = 2;
    world.newestAsked = 0;
    svc.close();
    const el = render();
    svc.open('', 'changes');
    flush();
    check('the feed starts where the buffer is', svc._lastID() === 4211, String(svc._lastID()));

    // The daemon restarts. Its buffer now holds ids 1 and 2.
    world.diskEvents = [
        ev(1, { data: { folder: 'assets', label: 'Shared Art', action: 'modified', type: 'file', path: 'after_restart.blend', modifiedBy: KAI_SHORT } }),
        ev(2, { data: { folder: 'assets', label: 'Shared Art', action: 'modified', type: 'file', path: 'after_restart2.blend', modifiedBy: KAI_SHORT } })
    ];
    svc._pollChanges();
    flush();
    flush();
    check('an empty poll asks where the buffer has got to', world.newestAsked === 1, String(world.newestAsked));
    check('and a buffer behind us is a restart: the feed starts again', svc._lastID() === 2, String(svc._lastID()));
    check('showing what happened after it', el.text().indexOf('after_restart') !== -1, el.text().slice(0, 120));

    // A quiet afternoon is not a restart, and is not asked about every poll.
    svc._pollChanges();
    flush();
    check('the check is throttled', world.newestAsked === 1, String(world.newestAsked));
    svc.close();
}


console.log('\n-- the older versions tab');

{
    world.archive['assets'] = {
        folder: 'assets', versioning: true, files: 2, versions: 9, bytes: 4096, total: 2,
        rows: [
            { name: 'refs/chair_v3.blend', versions: 6, bytes: 3072, newest: '2026-08-26T09:14:00+09:00', oldest: '2026-08-20T09:14:00+09:00', deleted: false },
            { name: 'old_test.blend', versions: 3, bytes: 1024, newest: '2026-08-25T17:02:00+09:00', oldest: '2026-08-22T17:02:00+09:00', deleted: true }
        ]
    };
    world.fileVersions['refs/chair_v3.blend'] = [
        { versionTime: '2026-08-26T09:14:00+09:00', modTime: '2026-08-26T09:14:00+09:00', size: 2048 },
        { versionTime: '2026-08-25T17:02:00+09:00', modTime: '2026-08-25T17:02:00+09:00', size: 1024 }
    ];

    svc.close();
    const el = render();
    svc.open('assets', 'versions');
    flush();
    const text = el.text();

    check('the archive lists its files', text.indexOf('chair_v3.blend') !== -1);
    check('with a count of kept copies', text.indexOf('6 kept') !== -1, text.slice(0, 120));
    check('a file that is gone is tagged deleted', text.indexOf('deleted') !== -1);
    check('the summary states what the archive costs',
        text.indexOf('on disk') !== -1);
    check('and that it expires', text.indexOf('thirty days') !== -1);

    // Expanding one row fetches only that file's versions -- the whole point
    // of the fork's endpoint over upstream's.
    svc.toggle(world.archive['assets'].rows[0]);
    flush();
    const expanded = el.text();
    check('expanding a row lists its copies', expanded.indexOf('Restore') !== -1);

    // The sentence that makes Restore not frightening.
    check('and says the current file is kept',
        expanded.indexOf('archived first') !== -1, expanded.slice(-160));
}

{
    // A deleted file offers different words: putting a file back is not the
    // same act as rolling one back, and "Restore" for both hides that.
    svc.close();
    const el = render();
    svc.open('assets', 'versions');
    flush();
    world.fileVersions['old_test.blend'] = [
        { versionTime: '2026-08-25T17:02:00+09:00', modTime: '2026-08-25T17:02:00+09:00', size: 1024 }
    ];
    svc.toggle(world.archive['assets'].rows[1]);
    flush();
    check('a deleted file says put it back', el.text().indexOf('Put it back') !== -1);
}

console.log('\n-- pinning a copy');
{
    world.archive['assets'] = {
        folder: 'assets', versioning: true, files: 1, versions: 2, bytes: 3072, total: 1, pinned: 1,
        rows: [
            { name: 'refs/chair_v3.blend', versions: 2, bytes: 3072, newest: '2026-08-26T09:14:00+09:00', oldest: '2026-08-25T17:02:00+09:00', deleted: false, pinned: 1 }
        ]
    };
    world.fileVersions['refs/chair_v3.blend'] = [
        { versionTime: '2026-08-26T09:14:00+09:00', modTime: '2026-08-26T09:14:00+09:00', size: 2048, pinned: false },
        { versionTime: '2026-08-25T17:02:00+09:00', modTime: '2026-08-25T17:02:00+09:00', size: 1024, pinned: true }
    ];
    world.pin = null;
    world.posted = [];

    svc.close();
    const el = render();
    svc.open('assets', 'versions');
    flush();
    check('the summary says pinned copies are exempt from the cleanup',
        /except the\s+1\s+pinned/.test(el.text()), el.text().slice(0, 200));
    check('the row counts its pinned copies', el.text().indexOf('1 pinned') !== -1);

    const row = svc.state.archive.rows[0];
    svc.toggle(row);
    flush();
    const pins = el[0].querySelectorAll('.desuq-hist-pin');
    check('every copy has a pin toggle', pins.length === 2, String(pins.length));
    check('a pinned copy says so and is pressed',
        pins[1].textContent.indexOf('Pinned') !== -1 && pins[1].getAttribute('aria-pressed') === 'true' &&
        pins[1].classList.contains('is-on'));
    check('an unpinned one offers to pin',
        /\bPin\b/.test(pins[0].textContent) && pins[0].getAttribute('aria-pressed') === 'false');
    check('the note says pins are this computer only',
        el.text().replace(/\s+/g, ' ').indexOf('this computer only') !== -1);

    const first = svc.expandedFor(row).versions[0];
    svc.togglePin(row, first);
    flush();
    const sent = world.posted[0] && world.posted[0].body;
    check('pinning posts the file and its version time',
        world.posted.length === 1 && world.posted[0].url === 'rest/folder/pin' &&
        sent.folder === 'assets' && sent.file === 'refs/chair_v3.blend' &&
        sent.versionTime === '2026-08-26T09:14:00+09:00' && sent.pinned === true,
        JSON.stringify(world.posted[0]));
    check('and the copy, the row and the header all count it without a reload',
        first.pinned === true && row.pinned === 2 && svc.state.archive.pinned === 2,
        [first.pinned, row.pinned, svc.state.archive.pinned].join(' '));
    check('the notice says what a pin does', svc.state.notice.indexOf('leave it alone') !== -1, svc.state.notice);

    // A refusal leaves everything as it was, and says why.
    const $q = injector.get('$q');
    world.pin = function () { return $q.reject({ status: 404, data: 'that copy is no longer in the archive\n' }); };
    const second = svc.expandedFor(row).versions[1];
    svc.togglePin(row, second);
    flush();
    check('a refused unpin changes nothing',
        second.pinned === true && row.pinned === 2 && svc.state.archive.pinned === 2);
    check('and passes the server\'s reason on',
        svc.state.notice.indexOf('no longer in the archive') !== -1, svc.state.notice);
    check('and the buttons are usable again', svc.state.busy === false);
    world.pin = null;
}

{
    // The empty archive is the state the developer's own machine is in today,
    // and "no history" must not read as "history is broken".
    world.archive['assets'] = { folder: 'assets', versioning: true, files: 0, versions: 0, bytes: 0, total: 0, rows: [] };
    svc.close();
    const el = render();
    svc.open('assets', 'versions');
    flush();
    const text = el.text();
    check('an empty archive explains itself',
        text.indexOf('replaced or deleted') !== -1, text.slice(0, 140));
    check('and reassures rather than alarms',
        text.indexOf('Nothing is lost') !== -1);
}

{
    // A folder with versioning switched off is a *different* fact, and the
    // one that actually matters: an empty list here means there is no safety
    // net, not that nothing has happened yet. Collapsing the two is how
    // somebody comes away believing they are covered when they are not. Found
    // live -- the test pair's folder has no versioner, and the server used to
    // answer that with a 500 the screen rendered as "could not read".
    world.archive['assets'] = { folder: 'assets', versioning: false, files: 0, versions: 0, bytes: 0, total: 0, rows: [] };
    svc.close();
    const el = render();
    svc.open('assets', 'versions');
    flush();
    const text = el.text();
    check('versioning off says so', text.indexOf('not being kept') !== -1, text.slice(0, 140));
    check('and warns that deletion is permanent', text.indexOf('permanent') !== -1);
    check('without claiming nothing has happened yet',
        text.indexOf('Nothing is lost') === -1);
}


// =========================================================================

console.log('\n-- previewable is the server\'s extension list');
{
    const previewable = window.previewable;
    const previewURL = window.previewURL;
    check('a texture is previewable', previewable('refs/wood.PNG') && previewable('a.jpg') && previewable('b.gif'));
    check('a blend file is, and its .blend1 backup', previewable('scene.blend') && previewable('Scenes/cabin.BLEND1'));
    check('other files are not', !previewable('notes') && !previewable('') && !previewable('scene.blend2') && !previewable('a.blend.txt'));
    // The extension gate is here so that a folder of .blend files costs no
    // requests at all -- the server would refuse them one at a time.
    check('the url carries folder, file and version',
        previewURL('assets', 'refs\\a.png', '2026-08-26T09:14:00+09:00') ===
        'preview/?folder=assets&file=refs%5Ca.png&version=2026-08-26T09%3A14%3A00%2B09%3A00',
        previewURL('assets', 'refs\\a.png', '2026-08-26T09:14:00+09:00'));
    check('and leaves version off for the live file',
        previewURL('assets', 'a.png').indexOf('version') === -1);
}

console.log('\n-- an archived conflict copy is named after the file it came from');
{
    const co = window.conflictOriginal;
    check('a conflict copy resolves to its original',
        co('texture4.sync-conflict-20260826-155856-V7OXBJ3.png') === 'texture4.png',
        co('texture4.sync-conflict-20260826-155856-V7OXBJ3.png'));
    check('with a path in front of it',
        co('refs/chair.sync-conflict-20260826-155856-V7OXBJ3.blend') === 'chair.blend');
    // The extension sits after the stamp, so a file with none is the case a
    // last-dot split gets wrong -- the same trap as the server's parser.
    check('and no extension at all still works',
        co('notes.sync-conflict-20260826-155856-V7OXBJ3') === 'notes');
    check('an ordinary name is left alone', co('texture4.png') === '');
    check('and is what the row shows', window.prettyArchive('refs/texture4.png') === 'texture4.png');
}

console.log('\n-- thumbnails in the archive');
{
    world.archive['assets'] = {
        folder: 'assets', versioning: true, files: 2, versions: 4, bytes: 4096, total: 2,
        rows: [
            { name: 'wood_albedo.png', versions: 2, bytes: 2048, newest: '2026-08-26T09:14:00+09:00', oldest: '2026-08-20T09:14:00+09:00', deleted: false },
            { name: 'scene.blend', versions: 2, bytes: 2048, newest: '2026-08-25T09:14:00+09:00', oldest: '2026-08-20T09:14:00+09:00', deleted: false },
            { name: 'notes.txt', versions: 1, bytes: 10, newest: '2026-08-25T09:14:00+09:00', oldest: '2026-08-25T09:14:00+09:00', deleted: false }
        ]
    };
    world.fileVersions['wood_albedo.png'] = [
        { versionTime: '2026-08-26T09:14:00+09:00', modTime: '2026-08-26T09:14:00+09:00', size: 2048 }
    ];
    svc.close();
    const el = render();
    svc.open('assets', 'versions');
    flush();

    // innerHTML escapes the ampersands in the query string, so the raw URL
    // never appears verbatim -- compare against the unescaped form.
    const src = () => el.html().replace(/&amp;/g, '&');
    check('a texture row draws its picture',
        src().indexOf('preview/?folder=assets&file=wood_albedo.png') !== -1);
    // The server reads the preview Blender embeds in the file
    // (lib/api/desuq_blendthumb.go), so a .blend row asks too.
    check('a blend file row asks for its preview picture',
        src().indexOf('preview/?folder=assets&file=scene.blend') !== -1);
    check('a file with no preview asks for nothing',
        el.html().indexOf('file=notes.txt') === -1);
    // The path is the load-bearing part: under rest/ every one of these would
    // be a 403 in a real browser and pass here.
    check('and the thumbnails are not asked for under rest/',
        el.html().indexOf('rest/folder/preview') === -1);

    svc.toggle(world.archive['assets'].rows[0]);
    flush();
    check('and each archived copy gets one, by its version time',
        el.html().indexOf('version=2026-08-26T09') !== -1, '');
}

console.log('\n-- the conflicts tab');
{
    world.conflicts = {
        total: 1,
        folders: [{
            folder: 'assets', label: 'Shared Art', count: 1, bytes: 2048,
            versioning: true, receiveOnly: false,
            rows: [{
                conflict: 'refs/chair.sync-conflict-20260824-032916-X7Z653J.png',
                name: 'refs/chair.png',
                when: '2026-08-24T03:29:16+09:00',
                current: { present: true, size: 4096, modified: '2026-08-24T03:29:00+09:00', by: 'Kai', mine: false },
                aside: { present: true, size: 2048, modified: '2026-08-24T03:20:00+09:00', by: 'this computer', mine: true }
            }]
        }]
    };
    svc.close();
    const el = render();
    svc.open('assets', 'conflicts');
    flush();
    const text = el.text();

    check('the file is named as a person would say it', text.indexOf('chair.png') !== -1, text.slice(0, 160));
    check('both sides are labelled', text.indexOf('In use now') !== -1 && text.indexOf('Set aside') !== -1);
    check('with a size each', text.indexOf('4 KiB') !== -1 && text.indexOf('2 KiB') !== -1);
    // Who wrote each copy comes from the index, never from the device id in
    // the conflict file's name -- that one belongs to whoever's edit won.
    check('and who wrote each one', text.indexOf('Kai') !== -1 && text.indexOf('you') !== -1);
    check('both choices are offered',
        text.indexOf('Keep this one') !== -1 && text.indexOf('Use this one instead') !== -1);
    check('and the promise that makes them safe',
        text.indexOf('the other copy goes to') !== -1);
    check('the two copies are shown as pictures',
        el.html().indexOf('file=refs%2Fchair.png') !== -1 &&
        el.html().indexOf('file=refs%2Fchair.sync-conflict') !== -1);

    // Resolving.
    world.posted = [];
    world.resolve = function () {
        return injector.get('$q').when({ data: { archived: true, remaining: 0 } });
    };
    world.conflicts = { total: 0, folders: [] };
    svc.resolveConflict('assets', world.conflicts.folders[0] ||
        { conflict: 'refs/chair.sync-conflict-20260824-032916-X7Z653J.png', name: 'refs/chair.png' }, 'aside');
    flush();
    check('resolving posts the decision',
        world.posted.length === 1 && world.posted[0].body.keep === 'aside',
        JSON.stringify(world.posted[0] && world.posted[0].body));
    check('and says where the other copy went',
        svc.state.notice.indexOf('Older versions') !== -1, svc.state.notice);
}

{
    // A folder with no versioning cannot keep the promise, so the server
    // refuses and the screen turns into a question rather than an error.
    const $q = injector.get('$q');
    world.conflicts = {
        total: 1,
        folders: [{
            folder: 'assets', label: 'Shared Art', count: 1, bytes: 2048,
            versioning: false, receiveOnly: true,
            rows: [{
                conflict: 'scene.sync-conflict-20260824-032916-X7Z653J.blend',
                name: 'scene.blend',
                when: '2026-08-24T03:29:16+09:00',
                current: { present: true, size: 4096, modified: '2026-08-24T03:29:00+09:00', by: 'Kai', mine: false },
                aside: { present: true, size: 2048, modified: '2026-08-24T03:20:00+09:00', by: 'this computer', mine: true }
            }]
        }]
    };
    svc.close();
    const el = render();
    svc.open('assets', 'conflicts');
    flush();

    check('a folder with no history says the loss is permanent',
        el.text().indexOf('deleted for good') !== -1, el.text().slice(0, 200));
    check('and a receive-only folder says the choice stays here',
        el.text().indexOf('only receives') !== -1);

    world.resolve = function () {
        return $q.reject({ status: 409, data: 'this folder keeps no history, so the copy you do not keep would be deleted permanently' });
    };
    svc.resolveConflict('assets', world.conflicts.folders[0].rows[0], 'current');
    flush();
    check('the refusal becomes a question, not a failure',
        svc.state.confirm !== null && svc.state.notice === '',
        svc.state.notice);
    flush();
    check('and the question is on screen', el.text().indexOf('cannot be undone') !== -1);

    world.resolve = function () { return $q.when({ data: { archived: false, remaining: 0 } }); };
    world.posted = [];
    svc.resolveConflict('assets', world.conflicts.folders[0].rows[0], 'current', true);
    flush();
    check('answering it forces the call through',
        world.posted.length === 1 && world.posted[0].body.force === true);
    check('and the wording drops the promise it cannot keep',
        svc.state.notice.indexOf('has been deleted') !== -1, svc.state.notice);
}

{
    // Nothing in conflict is the normal state and must not read as an error.
    world.conflicts = { total: 0, folders: [] };
    svc.close();
    const el = render();
    svc.open('assets', 'conflicts');
    flush();
    check('an empty conflicts tab is reassuring',
        el.text().indexOf('Nothing is in conflict') !== -1, el.text().slice(0, 140));
    check('and explains what would put something there',
        el.text().indexOf('before their computers can talk') !== -1);
}

console.log('\n-- Explorer\'s "Show history" opens straight onto one file');
// custom/tray/explorer.go opens /?desuq-history=<folder>&file=<path>.
{
    svc.close();
    check('with no parameters it does nothing', svc._openFromURL() === false && !svc.state.open);
    window.history.pushState(null, '', '/?desuq-history=assets&file=scenes%2Fcabin.blend');
    const opened = svc._openFromURL();
    check('with them it opens', opened === true && svc.state.open);
    check('on the versions tab of that folder',
        svc.state.tab === 'versions' && svc.state.folder === 'assets', svc.state.tab + ' ' + svc.state.folder);
    check('searched for the file by name', svc.state.search === 'cabin.blend', svc.state.search);
    check('and takes the parameters off the address, so a reload does not reopen it',
        window.location.search === '', window.location.search);
    flush();
    svc.close();
}

console.log('\n-- no green anywhere');
{
    const css = fs.readFileSync(path.join(desuq, 'history.css'), 'utf8');
    check('history.css references no success token', css.indexOf('--v-success') === -1);
}

console.log('');
if (failures) {
    console.error(failures + ' check(s) failed');
    process.exit(1);
}
console.log('all checks passed');
