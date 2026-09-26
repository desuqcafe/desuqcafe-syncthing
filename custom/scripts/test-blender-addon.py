"""Checks for the Blender add-on's client (custom/blender/desuq_syncthing).

    python custom/scripts/test-blender-addon.py

Needs any Python 3.9+ and nothing else: client.py does not import bpy, so the
parts that decide what a person is told, and which folder a file is in, are
tested here without Blender. What only Blender can show -- the handlers
firing on open, save and close -- is test-blender-live.ps1, run by hand,
because CI has no Blender.
"""

import importlib.util
import os
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
ADDON = os.path.join(HERE, "..", "blender", "desuq_syncthing")

spec = importlib.util.spec_from_file_location("desuq_client", os.path.join(ADDON, "client.py"))
dc = importlib.util.module_from_spec(spec)
spec.loader.exec_module(dc)

failures = 0


def check(name, ok, detail=""):
    global failures
    if ok:
        print("  ok   " + name)
    else:
        failures += 1
        print("  FAIL " + name + ("   " + str(detail) if detail else ""))


print("-- which folder a file is in")
root = tempfile.mkdtemp()
assets = os.path.join(root, "Assets")
assets2 = os.path.join(root, "Assets2")
inner = os.path.join(assets, "Library")
folders = [
    {"id": "a", "label": "Project Assets", "path": assets},
    {"id": "b", "label": "Other", "path": assets2},
    {"id": "c", "label": "", "path": inner},
]
loc = dc.folder_for_path(folders, os.path.join(assets, "Scenes", "cabin.blend"))
check("a file two levels down", loc == ("a", "Project Assets", "Scenes/cabin.blend"), loc)
loc = dc.folder_for_path(folders, os.path.join(assets2, "x.blend"))
check("a sibling whose name starts the same is a different folder", loc == ("b", "Other", "x.blend"), loc)
loc = dc.folder_for_path(folders, os.path.join(inner, "rock.blend"))
check("a folder inside another: the inner one wins, labelled by id", loc == ("c", "c", "rock.blend"), loc)
check("outside every folder is None", dc.folder_for_path(folders, os.path.join(root, "loose.blend")) is None)
check("the folder itself is not a file in it", dc.folder_for_path(folders, assets) is None)
if os.name == "nt":
    loc = dc.folder_for_path(folders, os.path.join(assets.upper(), "Scenes", "Cabin.blend"))
    check("Windows paths compare without case, and keep the file's own case",
          loc is not None and loc[2] == "Scenes/Cabin.blend", loc)

print("-- reading this computer's settings")
home = tempfile.mkdtemp()
with open(os.path.join(home, "config.xml"), "w", encoding="utf-8") as f:
    f.write('<configuration><gui enabled="true" tls="false"><address>0.0.0.0:8384</address>'
            '<apikey>k3y</apikey></gui></configuration>')
ep = dc.read_endpoint(home)
check("a GUI on every interface is reached on loopback", ep.base_url == "http://127.0.0.1:8384", ep.base_url)
check("with the API key", ep.api_key == "k3y")
try:
    dc.read_endpoint(tempfile.mkdtemp())
    check("no settings file is said plainly", False)
except dc.Unavailable as e:
    check("no settings file is said plainly", "not set up" in str(e), e)
os.environ["DESUQ_HOME"] = home
check("DESUQ_HOME points it elsewhere", dc.default_home() == home)
del os.environ["DESUQ_HOME"]
check("otherwise it is the installer's data directory",
      dc.default_home().endswith(os.sep + "desuqcafe-syncthing"), dc.default_home())

print("-- a Syncthing that is not running")
c = dc.Client(dc.Endpoint("http://127.0.0.1:9", "x"))
try:
    c.claims("a")
    check("is a sentence, not a traceback", False)
except dc.Unavailable as e:
    check("is a sentence, not a traceback", "not running" in str(e), e)

print("-- what opening a file says")
rel = "Scenes/cabin.blend"
claims = {"claims": [
    {"path": rel, "mine": False, "name": "Kai"},
    {"path": "other.blend", "mine": False, "name": "Mia"},
]}
lines = dc.opening_warnings("cabin.blend", claims, {"behind": False}, [], rel)
check("somebody else's mark is said, with what happens if you both save",
      lines[:1] == ["Kai is working on cabin.blend."] and "copy of your own" in lines[1], lines)
check("a mark on another file is not", not any("Mia" in l for l in lines))
lines = dc.opening_warnings("cabin.blend", {"claims": []}, {"behind": True, "modifiedBy": "Kai"}, [], rel)
check("a newer version on its way is said", any("still on its way" in l for l in lines), lines)
notes = [
    {"current": False, "mine": False, "name": "Kai", "text": "old", "at": "2026-09-26T10:00:00Z"},
    {"current": True, "mine": False, "name": "Kai", "text": "put the lighting back", "at": "2026-09-26T09:00:00Z"},
]
lines = dc.opening_warnings("cabin.blend", {"claims": []}, {}, notes, rel)
check("the note on the version you are opening is shown", lines == ['Kai, about this version: "put the lighting back"'], lines)
notes[1]["mine"] = True
check("your own note is not read back to you", dc.opening_warnings("cabin.blend", {"claims": []}, {}, notes, rel) == [])
check("nothing to say is nothing", dc.opening_warnings("cabin.blend", {"claims": [{"path": rel, "mine": True}]}, {}, [], rel) == [])
handing = {"claims": [{"path": rel, "mine": False, "name": "Kai", "handingTo": {"mine": True}}]}
lines = dc.opening_warnings("cabin.blend", handing, {}, [], rel)
check("a file being handed to you says so, not 'leave it'", lines == ["Kai is handing cabin.blend to you."], lines)

print("-- who has this")
who = {"exists": True, "behind": False, "peers": [
    {"name": "Kai", "has": True, "connected": True},
    {"name": "Mia", "has": False, "connected": False},
    {"name": "Ana", "heldBack": True},
]}
lines = dc.who_has_text("cabin.blend", {"claims": [{"path": rel, "mine": True}]}, who, notes, rel)
check("who has it, who is offline, who is not keeping it, and why it changed",
      lines == ["You are working on it.", "Has your version: Kai.", "Offline, without your version yet: Mia.",
                "Not keeping this file: Ana.", 'Why (you): "put the lighting back"'], lines)

print("-- the add-on's manifest")
try:
    import tomllib
except ImportError:
    tomllib = None
if tomllib:
    with open(os.path.join(ADDON, "blender_manifest.toml"), "rb") as f:
        m = tomllib.load(f)
    need = ["schema_version", "id", "version", "name", "tagline", "maintainer", "type", "blender_version_min", "license"]
    check("has every field Blender requires", all(k in m for k in need), [k for k in need if k not in m])
    check("asks for the network and files permissions it uses",
          set((m.get("permissions") or {}).keys()) == {"network", "files"}, m.get("permissions"))
    check("its id is the package name", m.get("id") == "desuq_syncthing")
    check("the tagline fits Blender's 64 characters", len(m.get("tagline", "")) <= 64)
else:
    print("  (manifest not checked: this Python has no tomllib)")

print()
if failures:
    print("%d check(s) failed" % failures)
    sys.exit(1)
print("all checks passed")
