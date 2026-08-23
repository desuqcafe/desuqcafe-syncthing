// Drives the selective-sync picker through real Angular, real fancytree and a
// real Syncthing, and asserts on what ends up on disk.
//
//     .\custom\scripts\start-test-pair.ps1 -Fresh -WithFolder
//     npm install jsdom          (once, anywhere on the path)
//     node custom/scripts/test-selective-render.js
//
// jsdom is not a dependency of this repository and the build does not need it,
// so this is a developer tool, in the same vein as test-handshake-render.js.
//
// The point of doing it this way rather than with a mocked $http is that the
// interesting parts of this feature are not in the JavaScript. They are in
// what Syncthing does with what the JavaScript writes: that "*" really does
// hold every byte back while the index still arrives, that an anchored
// exclusion really does expand to the directory *and* its contents, and that
// a file which is merely ignored is never fetched rather than fetched and
// hidden. None of that can be asserted against a stub.
//
// So $http here is a thin shim onto the running instance B, and the assertions
// at the end read the filesystem.

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
const gui = path.join(REPO, 'gui', 'default');
const desuq = path.join(gui, 'syncthing', 'desuq');

// Instance B from start-test-pair.ps1 -- the side that receives the share.
const BASE = process.env.DESUQ_TEST_URL || 'http://127.0.0.1:8391';
const KEY = process.env.DESUQ_TEST_KEY || 'desuqtestkeyBBBBBBBBBBBBBBBBBBBB';
// Instance A, the side that offers it. Needed because this test builds its own
// fixture tree over there rather than relying on whatever happens to be in the
// shared folder -- start-test-pair only makes six flat files, and a picker
// test with no directories in it would assert almost nothing.
const SENDER = process.env.DESUQ_TEST_SENDER_URL || 'http://127.0.0.1:8390';
const SENDER_KEY = process.env.DESUQ_TEST_SENDER_KEY || 'desuqtestkeyAAAAAAAAAAAAAAAAAAAA';
const FOLDER = 'assets-test';
const PAIR_ROOT = path.join(
    process.env.TEMP || process.env.TMP || '/tmp', 'desuq-syncthing-testpair');
const DATA = path.join(PAIR_ROOT, 'dataB', 'assets');
const SOURCE = path.join(PAIR_ROOT, 'dataA', 'assets');

let failures = 0;
function check(name, ok, detail) {
    console.log((ok ? '  ok   ' : '  FAIL ') + name + (detail ? '   ' + detail : ''));
    if (!ok) { failures++; }
}
function sleep(ms) { return new Promise(r => setTimeout(r, ms)); }

// --------------------------------------------------------------------- setup

const dom = new JSDOM(
    '<!DOCTYPE html><html><body><div id="host">' +
    '<desuq-selective-modal></desuq-selective-modal>' +
    '</div></body></html>',
    { runScripts: 'outside-only', pretendToBeVisual: true, url: BASE + '/' }
);
const { window } = dom;
global.window = window;
global.document = window.document;
global.navigator = window.navigator;

function load(file) {
    window.eval(fs.readFileSync(file, 'utf8'));
}

load(path.join(gui, 'vendor', 'jquery', 'jquery-3.7.1.js'));
load(path.join(gui, 'vendor', 'fancytree', 'jquery.fancytree-all-deps.js'));
load(path.join(gui, 'vendor', 'angular', 'angular.js'));

const angular = window.angular;
angular.module('syncthing.core', []);

// app.js defines unitPrefixed, which the binary filter used by the modal
// template needs, and urlbase. Loading the whole of it would pull in the
// router, so take just the two.
window.eval('var urlbase = "rest";');
const appjs = fs.readFileSync(path.join(gui, 'syncthing', 'app.js'), 'utf8');
window.eval(appjs.substring(appjs.indexOf('function unitPrefixed')));

load(path.join(gui, 'syncthing', 'core', 'binaryFilter.js'));
load(path.join(desuq, 'diskFreeDirective.js'));
load(path.join(desuq, 'selectiveSync.js'));

// The modal template is fetched by templateUrl; hand it over directly rather
// than making jsdom serve files.
const templateHtml = fs.readFileSync(
    path.join(desuq, 'selectiveSyncModalView.html'), 'utf8');

// $http, onto the real instance. Only the four verbs the picker uses.
angular.module('syncthing.core')
    .config(['$provide', function ($provide) {
        $provide.decorator('$http', ['$q', '$templateCache', function ($q, $templateCache) {
            function call(method, url, data, params) {
                const u = new URL(url.replace(/^rest/, BASE + '/rest'));
                if (params) {
                    Object.keys(params).forEach(k => u.searchParams.set(k, params[k]));
                }
                const d = $q.defer();
                fetch(u, {
                    method: method,
                    headers: Object.assign({ 'X-API-Key': KEY },
                        data ? { 'Content-Type': 'application/json' } : {}),
                    body: data ? JSON.stringify(data) : undefined
                }).then(async r => {
                    const text = await r.text();
                    let body = text;
                    try { body = JSON.parse(text); } catch (e) { /* not JSON */ }
                    if (r.ok) { d.resolve({ data: body, status: r.status }); }
                    else { d.reject({ data: body, status: r.status }); }
                }).catch(e => d.reject({ data: String(e), status: 0 }));
                return d.promise;
            }
            const $http = {
                get: function (url, cfg) {
                    // $templateRequest fetches templateUrl through $http with
                    // cache: $templateCache, and does not consult the cache
                    // itself -- so a stub that ignores the option would send
                    // the template off to Syncthing's REST API.
                    const cached = $templateCache.get(String(url));
                    if (cached !== undefined) {
                        return $q.when({ data: cached, status: 200 });
                    }
                    // Fault injection for one branch that cannot be reached
                    // honestly: the picker drops to a directories-only tree
                    // past twenty thousand files, and creating twenty
                    // thousand files on the other instance would take longer
                    // than the rest of this test put together. Only the count
                    // is faked; the tree that comes back is the real one.
                    if (inflateFileCount && /db\/status/.test(String(url))) {
                        return call('GET', url, null, cfg && cfg.params)
                            .then(function (r) {
                                r.data.globalFiles = 100000;
                                return r;
                            });
                    }
                    return call('GET', url, null, cfg && cfg.params);
                },
                post: (url, data) => call('POST', url, data),
                patch: (url, data) => call('PATCH', url, data)
            };
            return $http;
        }]);
    }])
    .run(['$templateCache', function ($templateCache) {
        $templateCache.put('syncthing/desuq/selectiveSyncModalView.html', templateHtml);
    }]);

angular.bootstrap(window.document.body, ['syncthing.core']);

const injector = angular.element(window.document.body).injector();
const svc = injector.get('desuqSelective');
const $rootScope = injector.get('$rootScope');
const host = window.document.getElementById('host');

// jsdom has no layout and Bootstrap's modal plugin is not loaded, so
// $().modal() does not exist. The picker only ever uses it to show and hide;
// stub it and record, so the show/hide sequencing can still be asserted.
const modalCalls = [];
window.jQuery.fn.modal = function (arg) {
    modalCalls.push(typeof arg === 'string' ? arg : 'show');
    return this;
};

function digest() {
    try { $rootScope.$digest(); } catch (e) { /* already in one */ }
}

// $timeout here is the real one, so waiting means waiting.
async function settle(ms) {
    const until = Date.now() + ms;
    while (Date.now() < until) {
        await sleep(50);
        digest();
    }
}

async function waitFor(what, predicate, timeoutMs) {
    const until = Date.now() + timeoutMs;
    while (Date.now() < until) {
        digest();
        if (predicate()) { return true; }
        await sleep(150);
    }
    check(what, false, 'timed out after ' + timeoutMs + 'ms (phase=' +
        svc.state.phase + (svc.state.error ? ', error=' + svc.state.error : '') + ')');
    return false;
}

function call(base, key, method, p, body) {
    return fetch(base + p, {
        method: method,
        headers: Object.assign({ 'X-API-Key': key },
            body ? { 'Content-Type': 'application/json' } : {}),
        body: body ? JSON.stringify(body) : undefined
    }).then(async r => {
        const t = await r.text();
        try { return JSON.parse(t); } catch (e) { return t; }
    });
}

function api(method, p, body) { return call(BASE, KEY, method, p, body); }
function sender(method, p, body) { return call(SENDER, SENDER_KEY, method, p, body); }

// Deterministic bytes, so a rerun does not depend on the machine's entropy --
// but *different* per file, because Syncthing reconstructs a file from blocks
// the receiver already holds rather than transferring it, and identical
// payloads would quietly stop being transfers at all.
function payload(size, seed) {
    const b = Buffer.alloc(size);
    let x = seed >>> 0;
    for (let i = 0; i < size; i++) {
        x = (x * 1664525 + 1013904223) >>> 0;
        b[i] = x >>> 24;
    }
    return b;
}

// The fixture the picker is tested against: nested directories, a file at the
// root, and a name that is itself a glob for the file next to it.
async function buildFixture() {
    const dirs = ['Characters/Hero', 'Characters/Villain', 'Environments/Forest',
        'Textures/Source', 'Textures/Baked'];
    let seed = 1;
    for (const d of dirs) {
        fs.mkdirSync(path.join(SOURCE, d), { recursive: true });
        for (const n of ['asset1.png', 'asset2.png']) {
            fs.writeFileSync(path.join(SOURCE, d, n), payload(512 * 1024, seed++));
        }
    }
    fs.writeFileSync(path.join(SOURCE, 'README.txt'), 'top level file');
    fs.writeFileSync(path.join(SOURCE, BRACKET_NAME), payload(256 * 1024, seed++));

    await sender('POST', '/rest/db/scan?folder=' + FOLDER);
    for (let i = 0; i < 40; i++) {
        await sleep(1000);
        const s = await sender('GET', '/rest/db/status?folder=' + FOLDER);
        if (s.state === 'idle' && s.localFiles >= 18) { return s; }
    }
    throw new Error('the sender never finished indexing the fixture');
}

function tree() {
    return window.jQuery('#desuqSelectiveTree').fancytree('getTree');
}

// A fixture whose own name is a glob for its neighbour: unescaped,
// "asset[1].png" also matches "asset1.png".
//
// Which character does the escaping is decided by lib/ignore at init and is
// platform-dependent -- backslash normally, but pipe on Windows, where
// backslash is the path separator. The expected pattern is therefore built
// from the same fact rather than hard-coded, and asserted against the running
// server's own answer below.
const BRACKET_NAME = 'Textures/Baked/asset[1].png';
let ESC = null;
let BRACKET_PATTERN = null;
let inflateFileCount = false;

// The block markers are comments, and Syncthing's comment prefix is "//", so
// "starts with a slash" alone would sweep them up with the exclusions.
function exclusions(lines) {
    return (lines || [])
        .filter(l => l.charAt(0) === '/' && l.charAt(1) !== '/')
        .sort();
}

// ---------------------------------------------------------------------- main

async function main() {
    console.log('Selective sync, through Angular and fancytree, against ' + BASE);

    const ping = await api('GET', '/rest/system/ping').catch(() => null);
    const pingA = await sender('GET', '/rest/system/ping').catch(() => null);
    if (!ping || !ping.ping || !pingA || !pingA.ping) {
        console.error('The test pair is not answering on ' + SENDER + ' and ' + BASE + '.');
        console.error('Run:  .\\custom\\scripts\\start-test-pair.ps1 -Fresh -WithFolder');
        process.exit(2);
    }

    const fixture = await buildFixture();
    console.log('  (fixture on the sender: ' + fixture.localFiles + ' files, ' +
        fixture.localDirectories + ' directories)');

    // Start from nothing, the way a modeller who has just been offered the
    // share does.
    await api('DELETE', '/rest/config/folders/' + FOLDER);
    await sleep(2000);
    fs.rmSync(DATA, { recursive: true, force: true });

    // Seed the default ignores the installer writes. These live outside the
    // picker's managed block and must come through every rewrite untouched;
    // losing them would quietly turn the .blend1 churn back on.
    //
    // Deliberately without the "#include team.stignore" line from
    // DEPLOYMENT-3D-TEAM.md section 2. Putting an #include in the *defaults*
    // deadlocks every newly accepted folder -- the included file lives inside
    // the folder, so it cannot arrive until the folder syncs, and the folder
    // will not sync until the include resolves. That trap is covered on its
    // own below.
    const SEEDED = ['// Blender numbered backups', '(?d)*.blend[0-9]', '(?d)Thumbs.db'];
    await api('PUT', '/rest/config/defaults/ignores', { lines: SEEDED });

    const peers = await api('GET', '/rest/config/devices');
    const remote = peers.find(d => d.deviceID && d.name === 'Studio Workstation');
    if (!remote) {
        console.error('The offering device is not configured on B.');
        process.exit(2);
    }
    const me = (await api('GET', '/rest/system/status')).myID;

    const version = await api('GET', '/rest/system/version');
    ESC = version.os === 'windows' ? '|' : '\\';
    BRACKET_PATTERN = '/Textures/Baked/asset' + ESC + '[1' + ESC + '].png';
    console.log('  (' + version.os + ': ignore patterns escape with "' + ESC + '")');

    console.log('\n-- accepting the share through the picker');

    // Exactly the object saveFolder hands over.
    svc.acceptAndPick({
        id: FOLDER,
        label: 'Project Assets',
        path: DATA,
        type: 'sendreceive',
        _editing: 'new-pending',
        _desuqPick: true,
        devices: [
            { deviceID: me, introducedBy: '', encryptionPassword: '' },
            { deviceID: remote.deviceID, introducedBy: '', encryptionPassword: '' }
        ]
    });
    digest();

    check('the modal is asked to open', svc.state.open === true);

    // The whole safety claim: while the picker waits, the folder is running
    // and its index is arriving, but nothing at all is being downloaded.
    if (!await waitFor('the file list arrives from the other device',
        () => svc.state.phase === 'ready', 120000)) {
        finish();
        return;
    }

    const status = await api('GET', '/rest/db/status?folder=' + FOLDER);
    check('the index arrived', status.globalFiles > 0,
        status.globalFiles + ' files known');
    check('nothing was downloaded while choosing', status.localFiles === 0,
        'localFiles=' + status.localFiles + ' localBytes=' + status.localBytes);
    const onDisk = fs.existsSync(DATA)
        ? fs.readdirSync(DATA).filter(n => n !== '.stfolder' && n !== '.stignore')
        : [];
    check('the folder on disk is empty apart from Syncthing\'s own markers',
        onDisk.length === 0, JSON.stringify(onDisk));

    console.log('\n-- the tree');

    const t = tree();
    check('fancytree rendered', !!t);
    const roots = t.getRootNode().children.map(n => n.title);
    check('the whole remote tree is listed, not just what was pulled',
        roots.indexOf('Characters') >= 0 && roots.indexOf('Textures') >= 0 &&
        roots.indexOf('README.txt') >= 0,
        JSON.stringify(roots));

    check('everything starts ticked', svc.state.everything === true);
    check('directory weights are summed from their children',
        t.getNodeByKey('Characters').data.bytes ===
        t.getNodeByKey('Characters/Hero').data.bytes +
        t.getNodeByKey('Characters/Villain').data.bytes,
        String(t.getNodeByKey('Characters').data.bytes));
    check('the selected total matches the folder total',
        svc.state.selectedBytes === svc.state.totalBytes &&
        svc.state.totalBytes === status.globalBytes,
        svc.state.totalBytes + ' vs globalBytes ' + status.globalBytes);

    const sizeCells = host.querySelectorAll('.desuq-pick-size');
    check('per-row sizes are rendered', sizeCells.length > 0,
        sizeCells.length + ' rows');

    console.log('\n-- unticking');

    // A whole directory, one nested directory inside another, and a single
    // file: the three shapes the exclusion walk has to get right.
    t.getNodeByKey('Textures/Source').setSelected(false);
    t.getNodeByKey('Characters/Villain').setSelected(false);
    t.getNodeByKey('texture1.png').setSelected(false);
    // And one whose name is itself a glob for the file sitting next to it.
    // Unescaped, "asset[1].png" also matches "asset1.png", so unticking one
    // would silently drop both -- exactly the kind of quiet wrong answer this
    // picker exists to avoid.
    const bracket = t.getNodeByKey(BRACKET_NAME);
    check('the fixture with a glob character in its name is present', !!bracket,
        BRACKET_NAME);
    if (bracket) { bracket.setSelected(false); }
    await settle(400);

    check('unticking a child marks the parent partially selected',
        t.getNodeByKey('Textures').partsel === true &&
        t.getNodeByKey('Textures').selected === false);
    check('a sibling that was left alone stays fully selected',
        t.getNodeByKey('Environments').selected === true &&
        t.getNodeByKey('Textures/Baked/asset2.png').selected === true);
    check('the summary no longer says everything', svc.state.everything === false);

    const expectedBytes = status.globalBytes
        - t.getNodeByKey('Textures/Source').data.bytes
        - t.getNodeByKey('Characters/Villain').data.bytes
        - t.getNodeByKey('texture1.png').data.bytes
        - (bracket ? bracket.data.bytes : 0);
    check('the running total drops by exactly what was unticked',
        svc.state.selectedBytes === expectedBytes,
        svc.state.selectedBytes + ' expected ' + expectedBytes);

    console.log('\n-- applying');

    await svc.apply();
    await settle(300);

    const ign = await api('GET', '/rest/db/ignores?folder=' + FOLDER);
    const lines = ign.ignore || [];
    check('the picker wrote a marked block',
        lines.some(l => l.indexOf('desuqcafe selective sync') >= 0),
        JSON.stringify(lines));
    check('the unticked paths are anchored exclusions, minimal in number',
        exclusions(lines).join(' ') ===
        ['/Characters/Villain', BRACKET_PATTERN, '/Textures/Source',
            '/texture1.png'].sort().join(' '),
        JSON.stringify(exclusions(lines)));
    check('no ticked path was written',
        lines.indexOf('/Textures/Baked') < 0 && lines.indexOf('/Characters/Hero') < 0);
    check('the "*" that held everything back is gone',
        lines.indexOf('*') < 0, JSON.stringify(lines));
    check('the seeded rules survived, ahead of the managed block',
        SEEDED.every(l => lines.indexOf(l) >= 0 &&
            lines.indexOf(l) < lines.findIndex(x => x.indexOf('desuqcafe') >= 0)),
        JSON.stringify(lines));
    check('the glob characters in that name were escaped',
        lines.indexOf(BRACKET_PATTERN) >= 0, JSON.stringify(lines));

    check('the modal was closed', svc.state.open === false);
    check('backing out can no longer mean "sync everything"', svc.state.fresh === false);

    console.log('\n-- and what Syncthing then does with it');

    let s = null;
    for (let i = 0; i < 60; i++) {
        await sleep(2000);
        s = await api('GET', '/rest/db/status?folder=' + FOLDER);
        if (s.state === 'idle' && s.needBytes === 0 && s.localFiles > 0) { break; }
    }
    check('the folder reaches idle rather than sticking at out-of-sync',
        s && s.state === 'idle' && s.needBytes === 0,
        s ? s.state + ' need=' + s.needBytes : 'no status');

    const walk = (dir, base) => fs.readdirSync(dir, { withFileTypes: true })
        .flatMap(e => {
            if (e.name === '.stfolder' || e.name === '.stignore') { return []; }
            const rel = base ? base + '/' + e.name : e.name;
            return e.isDirectory() ? [rel].concat(walk(path.join(dir, e.name), rel)) : [rel];
        });
    const got = walk(DATA, '').sort();

    check('the ticked files were pulled',
        got.indexOf('Characters/Hero/asset1.png') >= 0 &&
        got.indexOf('Textures/Baked/asset1.png') >= 0 &&
        got.indexOf('README.txt') >= 0, JSON.stringify(got));
    // The distinction that matters: never written, not written then hidden.
    check('the unticked directory was never created at all',
        got.indexOf('Textures/Source') < 0 && got.indexOf('Characters/Villain') < 0,
        JSON.stringify(got.filter(p => /Source|Villain/.test(p))));
    check('the unticked file was never created',
        got.indexOf('texture1.png') < 0);
    check('its ticked siblings were', got.indexOf('texture2.png') >= 0);
    check('the escaped name excluded exactly itself and not its neighbour',
        got.indexOf(BRACKET_NAME) < 0 &&
        got.indexOf('Textures/Baked/asset1.png') >= 0,
        JSON.stringify(got.filter(p => p.indexOf('Textures/Baked/') === 0)));

    console.log('\n-- re-opening the picker on the folder it just wrote');

    const folders = await api('GET', '/rest/config/folders');
    const cfg = folders.find(f => f.id === FOLDER);
    svc.open(cfg);
    if (!await waitFor('it loads again', () => svc.state.phase === 'ready', 30000)) {
        finish();
        return;
    }
    const t2 = tree();
    check('last time\'s choice is read back off disk and shown unticked',
        t2.getNodeByKey('Textures/Source').selected === false &&
        t2.getNodeByKey('Characters/Villain').selected === false &&
        t2.getNodeByKey('texture1.png').selected === false);
    check('including the escaped one, so the escape survives a round trip',
        t2.getNodeByKey(BRACKET_NAME).selected === false);
    check('and everything else is still ticked',
        t2.getNodeByKey('Textures/Baked/asset1.png').selected === true &&
        t2.getNodeByKey('README.txt').selected === true);
    check('nothing was mistaken for a stale rule',
        svc.state.staleLines.length === 0, JSON.stringify(svc.state.staleLines));

    // Re-ticking one of them should pull it, which is the other direction the
    // exclusion walk has to handle.
    t2.getNodeByKey('Characters/Villain').setSelected(true);
    await settle(300);
    await svc.apply();
    await settle(300);

    const ign2 = await api('GET', '/rest/db/ignores?folder=' + FOLDER);
    check('re-ticking removes just that exclusion',
        exclusions(ign2.ignore).join(' ') ===
        [BRACKET_PATTERN, '/Textures/Source', '/texture1.png'].sort().join(' '),
        JSON.stringify(exclusions(ign2.ignore)));
    check('and the seeded rules are still there after a second rewrite',
        SEEDED.every(l => (ign2.ignore || []).indexOf(l) >= 0),
        JSON.stringify(ign2.ignore));

    for (let i = 0; i < 30; i++) {
        await sleep(2000);
        s = await api('GET', '/rest/db/status?folder=' + FOLDER);
        if (s.state === 'idle' && s.needBytes === 0) { break; }
    }
    check('and the newly ticked directory arrives',
        fs.existsSync(path.join(DATA, 'Characters', 'Villain', 'asset1.png')));

    console.log('\n-- backing out of a fresh accept must not strand the folder');

    // The dangerous state this guards: "*" left in the ignores, folder
    // reporting itself perfectly up to date, syncing nothing, forever.
    await api('DELETE', '/rest/config/folders/' + FOLDER);
    await sleep(2000);
    fs.rmSync(DATA, { recursive: true, force: true });

    svc.acceptAndPick({
        id: FOLDER, label: 'Project Assets', path: DATA, type: 'sendreceive',
        devices: [
            { deviceID: me, introducedBy: '', encryptionPassword: '' },
            { deviceID: remote.deviceID, introducedBy: '', encryptionPassword: '' }
        ]
    });
    digest();
    if (!await waitFor('it sets up again',
        () => ['waiting', 'loading', 'ready'].indexOf(svc.state.phase) >= 0, 30000)) {
        finish();
        return;
    }
    // The X, the backdrop, Escape -- all land here.
    svc.dismissed();
    await settle(1500);

    const ign3 = await api('GET', '/rest/db/ignores?folder=' + FOLDER);
    check('dismissing clears the "*" rather than leaving it in place',
        (ign3.ignore || []).indexOf('*') < 0, JSON.stringify(ign3.ignore));

    for (let i = 0; i < 40; i++) {
        await sleep(2000);
        s = await api('GET', '/rest/db/status?folder=' + FOLDER);
        if (s.state === 'idle' && s.needBytes === 0 && s.localFiles > 0) { break; }
    }
    check('so the folder syncs the lot, which is upstream\'s behaviour',
        s && s.localFiles === s.globalFiles,
        s ? s.localFiles + ' of ' + s.globalFiles : 'no status');

    console.log('\n-- a folder too big to list file by file falls back to directories');

    inflateFileCount = true;
    const folders2 = await api('GET', '/rest/config/folders');
    svc.open(folders2.find(f => f.id === FOLDER));
    if (!await waitFor('it loads', () => svc.state.phase === 'ready', 30000)) {
        finish();
        return;
    }
    const t3 = tree();
    check('the picker says so', svc.state.dirsOnly === true);
    check('and lists directories only',
        !!t3.getNodeByKey('Textures/Baked') && !t3.getNodeByKey('README.txt'),
        JSON.stringify(t3.getRootNode().children.map(n => n.title)));
    check('a directory counts as one pickable item rather than zero',
        svc.state.totalFiles === 8 && svc.state.selectedFiles === 8,
        svc.state.selectedFiles + ' of ' + svc.state.totalFiles);

    // Ticking a directory here has to mean the whole directory, files and all.
    svc.selectAll(false);
    t3.getNodeByKey('Characters/Hero').setSelected(true);
    await settle(400);
    check('the Sync Selection button is not wrongly disabled in this mode',
        svc.state.selectedFiles > 0, String(svc.state.selectedFiles));
    await svc.apply();
    await settle(300);
    const ign4 = await api('GET', '/rest/db/ignores?folder=' + FOLDER);
    check('excluding a directory excludes everything under it',
        exclusions(ign4.ignore).indexOf('/Environments') >= 0 &&
        exclusions(ign4.ignore).indexOf('/Characters/Villain') >= 0 &&
        exclusions(ign4.ignore).indexOf('/Characters/Hero') < 0,
        JSON.stringify(exclusions(ign4.ignore)));
    // Loose files at the root are invisible in this mode, so they must not be
    // silently excluded on the strength of a tree that never showed them.
    check('files the tree never showed are left alone',
        exclusions(ign4.ignore).indexOf('/README.txt') < 0 &&
        exclusions(ign4.ignore).indexOf('/texture2.png') < 0,
        JSON.stringify(exclusions(ign4.ignore)));
    inflateFileCount = false;

    console.log('\n-- ignore patterns Syncthing cannot parse are reported, not swallowed');

    // The trap from DEPLOYMENT-3D-TEAM.md section 2: an "#include" in the
    // defaults naming a file that lives inside the folder. The folder cannot
    // pull it, because the folder will not start until the include resolves.
    // Syncthing accepts the write and then stops the folder, so a picker that
    // only looked at the HTTP status would close on a success it did not have.
    await api('DELETE', '/rest/config/folders/' + FOLDER);
    await sleep(2000);
    fs.rmSync(DATA, { recursive: true, force: true });
    await api('PUT', '/rest/config/defaults/ignores',
        { lines: ['#include team.stignore'] });

    svc.acceptAndPick({
        id: FOLDER, label: 'Project Assets', path: DATA, type: 'sendreceive',
        devices: [
            { deviceID: me, introducedBy: '', encryptionPassword: '' },
            { deviceID: remote.deviceID, introducedBy: '', encryptionPassword: '' }
        ]
    });
    digest();
    const reported = await waitFor('the picker stops and says so',
        () => svc.state.phase === 'error', 30000);
    check('and names the real problem rather than "the request failed"',
        reported && /cannot read them/.test(svc.state.error || '') &&
        /team\.stignore/.test(svc.state.error || ''),
        svc.state.error);

    // Leave the instance in a state the next run can start from.
    await api('PUT', '/rest/config/defaults/ignores', { lines: SEEDED });
    await api('DELETE', '/rest/config/folders/' + FOLDER);

    finish();
}

function finish() {
    console.log('');
    if (failures) {
        console.log(failures + ' check(s) failed.');
        process.exit(1);
    }
    console.log('All checks passed.');
    process.exit(0);
}

main().catch(e => {
    console.error(e);
    process.exit(1);
});
