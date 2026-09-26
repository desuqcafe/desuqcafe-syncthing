"""The Blender add-on, inside a real Blender, against a live test pair.

Run by test-blender-live.ps1, which starts the pair and Blender. Not one of
the suites run-tests.ps1 runs: CI has no Blender. DESUQ_HOME points the
add-on at instance A; B is driven over REST as "the other person".
"""

import os
import sys
import time

import bpy

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "blender"))
import desuq_syncthing as ds  # noqa: E402

ds.register()
dc = ds.dc

shown = []
real_show = ds._show


def record(title, lines, icon="INFO"):
    shown.append((title, list(lines)))
    real_show(title, lines, icon)


ds._show = record

failures = 0


def check(name, ok, detail=""):
    global failures
    print(("  ok   " if ok else "  FAIL ") + name + ("" if ok or not detail else "   " + str(detail)), flush=True)
    if not ok:
        failures += 1


def wait(name, cond, secs=60):
    end = time.time() + secs
    while time.time() < end:
        try:
            if cond():
                check(name, True)
                return True
        except dc.Unavailable:
            pass
        time.sleep(0.5)
    check(name, False, "timed out")
    return False


A = dc.Client.local()
B = dc.Client(dc.Endpoint("http://127.0.0.1:8391", "desuqtestkeyBBBBBBBBBBBBBBBBBBBB"))
folder = A.get("/rest/config/folders")[0]
fid, root = folder["id"], folder["path"]
scene = os.path.join(root, "cabin.blend")
rel = "cabin.blend"


def mine_on_a():
    return dc.my_mark(A.claims(fid), rel)


def open_scene():
    bpy.ops.wm.open_mainfile(filepath=scene)


def close_scene():
    # Loading the factory file is "closing" as far as the handlers go:
    # load_pre fires for the file being left.
    bpy.ops.wm.read_homefile(use_factory_startup=True)


print("-- a new scene in the synced folder")
bpy.ops.wm.save_as_mainfile(filepath=scene)
# Asked of B's own index: A's whohas counts "missing on both sides" as
# delivered, which is true before A has even scanned the new file.
wait("B has it", lambda: B.who_has(fid, rel).get("exists") and not B.who_has(fid, rel).get("behind"))
close_scene()

print("-- opening it marks it as yours")
open_scene()
m = mine_on_a()
check("marked on open, as an automatic mark", m is not None and m.get("auto"), m)
wait("and B sees it", lambda: dc.others_marks(B.claims(fid), rel))
check("nothing was shown: nobody else had it", not shown, shown)

print("-- closing it unchanged takes the mark off")
close_scene()
check("gone on A", mine_on_a() is None)
wait("and on B", lambda: not dc.others_marks(B.claims(fid), rel))

print("-- somebody else has it open")
B.mark(fid, rel)
wait("A sees B's mark", lambda: dc.others_marks(A.claims(fid), rel))
shown.clear()
open_scene()
said = " ".join(" ".join(l) for _, l in shown)
check("opening says who is working on it", "is working on cabin.blend" in said, said)
check("and what happens if you both save", "copy of your own" in said, said)
check("and does not mark it over them", mine_on_a() is None)
close_scene()
B.release(fid, rel)
wait("B lets go", lambda: not dc.others_marks(A.claims(fid), rel))

print("-- saving keeps the mark past closing")
open_scene()
bpy.ops.mesh.primitive_cube_add()
bpy.ops.wm.save_mainfile()
close_scene()
m = mine_on_a()
check("a saved file stays marked; the tray's quiet-hours rule takes it from here", m is not None and m.get("auto"), m)

print("-- the menu")
open_scene()
r = bpy.ops.desuq.note(text="added a cube")
check("Say Why I Changed This runs", r == {"FINISHED"}, r)
n = [x for x in A.notes(fid, rel) if x.get("mine")]
check("and the note is on the version just saved", n and n[0]["text"] == "added a cube" and n[0]["current"], n)
wait("B gets the note", lambda: any(x.get("text") == "added a cube" for x in B.notes(fid, rel)))
check("I'm Working on This makes it a hand-made mark", bpy.ops.desuq.mark() == {"FINISHED"} and not mine_on_a().get("auto"))
close_scene()
check("which closing leaves alone", mine_on_a() is not None)
open_scene()
check("I'm Done with This takes it off", bpy.ops.desuq.done() == {"FINISHED"} and mine_on_a() is None)
close_scene()
shown.clear()
open_scene()
check("Who Has This? answers", bpy.ops.desuq.who() == {"FINISHED"} and shown and
      any("added a cube" in l for l in shown[-1][1]), shown)

print("-- a note on somebody else's version is refused with their reason")
close_scene()
B_root = B.get("/rest/config/folders")[0]["path"]
with open(os.path.join(B_root, rel), "ab") as f:
    f.write(b"\0")
B._request("POST", "/rest/db/scan", query={"folder": fid})
wait("B's save reaches A", lambda: A.who_has(fid, rel).get("modifiedBy") not in ("You", None, ""), 90)
open_scene()
try:
    A.note(fid, rel, "not mine")
    check("refused", False)
except dc.Unavailable as e:
    check("refused, saying whose it is", "somebody else" in str(e), e)

check("and opening while it is on its way does not mark it", mine_on_a() is None)

print("-- quitting Blender with a file open")
close_scene()
wait("A catches up with B's save", lambda: not A.who_has(fid, rel).get("behind"), 90)
open_scene()
check("opened and marked, ready for the exit check", mine_on_a() is not None)

print()
print("FAILURES=%d" % failures, flush=True)
# atexit releases the mark as Blender quits; the wrapper checks that.
