# desuqcafe Syncthing for Blender.
#
# Optional. Everything here can already be done from Explorer and the main
# screen; the add-on does two things neither can:
#
#   - It knows the moment a file is *opened*. The tray only learns about a
#     file when it is saved, and deliberately does not watch which programs
#     are running (custom/tray: that is the kind of code Defender flags). So
#     this is the one place that can say "Kai is working on this" before
#     you have changed anything, and mark the file as yours while you have it
#     open rather than from your first save.
#   - It can ask "why did you change this" where the change was made.
#
# It talks only to the Syncthing on this computer, with the key from its own
# settings file, and never changes a setting. See client.py, and
# custom/DEPLOYMENT-3D-TEAM.md section 33.

bl_info = {
    "name": "desuqcafe Syncthing",
    "author": "desuqcafe",
    "version": (1, 0, 0),
    "blender": (3, 6, 0),
    "location": "File > desuqcafe Syncthing",
    "description": "Says who is working on a file when you open it, and marks it as yours",
    "category": "System",
}

import atexit

import bpy
from bpy.app.handlers import persistent

from . import client as dc

# What this session did to the file that is open, so it can undo it on close
# and nothing else. A mark made by hand -- from the menu, Explorer or the main
# screen -- is never taken off by the add-on.
_session = {"path": "", "folder": "", "rel": "", "auto_marked": False, "saved": False}


def _reset():
    _session.update(path="", folder="", rel="", auto_marked=False, saved=False)


def _prefs():
    addon = bpy.context.preferences.addons.get(__package__)
    return addon.preferences if addon else None


def _show(title, lines, icon="INFO"):
    """A small popup, or the console when Blender has no window."""
    if not lines:
        return
    if bpy.app.background:
        print("[desuqcafe] " + title + ": " + " ".join(lines))
        return

    def draw(menu, _context):
        for line in lines:
            menu.layout.label(text=line)

    def later():
        wm = bpy.context.window_manager
        if wm and wm.windows:
            wm.popup_menu(draw, title=title, icon=icon)
        return None

    # A handler runs while the file is still loading; the popup has to wait
    # for a window to draw in.
    bpy.app.timers.register(later, first_interval=0.5)


def _release_if_ours():
    """Take off the mark this session made on open, if nothing has made it
    anybody's business since: it is still automatic, and the file was never
    saved here. A saved file is left to the tray, which takes automatic
    marks off after a few quiet hours -- the others should hear that it
    changed, not that it was let go of a second later."""
    if not _session["auto_marked"] or _session["saved"]:
        return
    try:
        c = dc.Client.local()
        mine = dc.my_mark(c.claims(_session["folder"]), _session["rel"])
        if mine and mine.get("auto") and not mine.get("handingTo"):
            c.release(_session["folder"], _session["rel"])
    except dc.Unavailable as e:
        print("[desuqcafe] could not take the mark off: %s" % e)


@persistent
def _on_load_pre(_dummy):
    _release_if_ours()
    _reset()


@persistent
def _on_load_post(_dummy):
    _reset()
    path = bpy.data.filepath
    if not path:
        return
    try:
        c = dc.Client.local()
        loc = c.locate(path)
        if loc is None:
            return
        folder, label, rel = loc
        _session.update(path=path, folder=folder, rel=rel)
        claims = c.claims(folder)
        who = c.who_has(folder, rel)
        notes = c.notes(folder, rel)
    except dc.Unavailable as e:
        # Not running is ordinary -- somebody opening a file with Syncthing
        # quit -- and not worth a popup every time.
        print("[desuqcafe] %s" % e)
        return

    name = bpy.path.basename(path)
    warnings = dc.opening_warnings(name, claims, who, notes, rel)
    others = dc.others_marks(claims, rel)
    if warnings:
        _show(name, warnings, icon="ERROR" if (others or who.get("behind")) else "INFO")

    # Mark it as yours -- unless somebody else has it, or you are about to
    # work on an old copy, or you already marked it. Marking over somebody
    # would make the warning above a formality.
    prefs = _prefs()
    if prefs is not None and not prefs.mark_on_open:
        return
    if others or who.get("behind") or dc.my_mark(claims, rel):
        return
    can = [f for f in (claims.get("folders") or []) if f.get("folder") == folder]
    if can and not can[0].get("canClaim", True):
        return
    try:
        c.mark(folder, rel, auto=True)
        _session["auto_marked"] = True
    except dc.Unavailable as e:
        print("[desuqcafe] could not mark %s: %s" % (rel, e))


@persistent
def _on_save_post(_dummy):
    if bpy.data.filepath and bpy.data.filepath == _session["path"]:
        _session["saved"] = True


def _current(op):
    """(client, folder, rel) for the open file, or None after reporting why."""
    path = bpy.data.filepath
    if not path:
        op.report({"ERROR"}, "Save the file into a synced folder first.")
        return None
    try:
        c = dc.Client.local()
        loc = c.locate(path)
    except dc.Unavailable as e:
        op.report({"ERROR"}, str(e))
        return None
    if loc is None:
        op.report({"ERROR"}, "This file is not in a folder desuqcafe Syncthing keeps.")
        return None
    return c, loc[0], loc[2]


class DESUQ_OT_mark(bpy.types.Operator):
    bl_idname = "desuq.mark"
    bl_label = "I'm Working on This"
    bl_description = "Tell everybody you share this folder with that you have this file open"

    def execute(self, _context):
        cur = _current(self)
        if cur is None:
            return {"CANCELLED"}
        c, folder, rel = cur
        try:
            c.mark(folder, rel, auto=False)
        except dc.Unavailable as e:
            self.report({"ERROR"}, str(e))
            return {"CANCELLED"}
        # Marked by hand now, so closing the file leaves it alone.
        _session["auto_marked"] = False
        self.report({"INFO"}, "Marked as yours. Everybody sharing this folder can see it.")
        return {"FINISHED"}


class DESUQ_OT_done(bpy.types.Operator):
    bl_idname = "desuq.done"
    bl_label = "I'm Done with This"
    bl_description = "Take your mark off this file"

    def execute(self, _context):
        cur = _current(self)
        if cur is None:
            return {"CANCELLED"}
        c, folder, rel = cur
        try:
            c.release(folder, rel)
        except dc.Unavailable as e:
            self.report({"ERROR"}, str(e))
            return {"CANCELLED"}
        _session["auto_marked"] = False
        self.report({"INFO"}, "Your mark is off. Use Say Why I Changed This if you changed it.")
        return {"FINISHED"}


class DESUQ_OT_note(bpy.types.Operator):
    bl_idname = "desuq.note"
    bl_label = "Say Why I Changed This"
    bl_description = "A sentence everybody sharing this folder sees beside this version of the file"

    text: bpy.props.StringProperty(name="Why", maxlen=500)

    def invoke(self, context, _event):
        # A note is about a saved version. With unsaved changes, the version
        # on disk is the previous save, and the note would land on that.
        if bpy.data.is_dirty:
            self.report({"ERROR"}, "Save first: a note is about the version you saved.")
            return {"CANCELLED"}
        return context.window_manager.invoke_props_dialog(self, width=420)

    def execute(self, _context):
        text = (self.text or "").strip()
        if not text:
            return {"CANCELLED"}
        cur = _current(self)
        if cur is None:
            return {"CANCELLED"}
        c, folder, rel = cur
        try:
            c.note(folder, rel, text)
        except dc.Unavailable as e:
            self.report({"ERROR"}, str(e))
            return {"CANCELLED"}
        self.report({"INFO"}, "Note saved. Everybody sharing this folder can see it.")
        return {"FINISHED"}


class DESUQ_OT_who(bpy.types.Operator):
    bl_idname = "desuq.who"
    bl_label = "Who Has This?"
    bl_description = "Who is working on this file, and whether everybody has your version"

    def execute(self, _context):
        cur = _current(self)
        if cur is None:
            return {"CANCELLED"}
        c, folder, rel = cur
        try:
            lines = dc.who_has_text(bpy.path.basename(rel), c.claims(folder), c.who_has(folder, rel),
                                    c.notes(folder, rel), rel)
        except dc.Unavailable as e:
            self.report({"ERROR"}, str(e))
            return {"CANCELLED"}
        _show(bpy.path.basename(rel), lines)
        return {"FINISHED"}


class DESUQ_MT_menu(bpy.types.Menu):
    bl_idname = "DESUQ_MT_menu"
    bl_label = "desuqcafe Syncthing"

    def draw(self, _context):
        layout = self.layout
        layout.operator(DESUQ_OT_mark.bl_idname, icon="GREASEPENCIL")
        layout.operator(DESUQ_OT_done.bl_idname, icon="CHECKMARK")
        layout.separator()
        layout.operator(DESUQ_OT_note.bl_idname, icon="TEXT")
        layout.operator(DESUQ_OT_who.bl_idname, icon="COMMUNITY")


class DESUQ_Preferences(bpy.types.AddonPreferences):
    bl_idname = __package__

    mark_on_open: bpy.props.BoolProperty(
        name="Mark files as mine when I open them",
        description="While a file is open here, everybody sharing its folder sees your name on it. "
                    "Taken off again when you close it without saving",
        default=True,
    )

    def draw(self, _context):
        self.layout.prop(self, "mark_on_open")


def _menu(self, _context):
    self.layout.separator()
    self.layout.menu(DESUQ_MT_menu.bl_idname)


_classes = (DESUQ_OT_mark, DESUQ_OT_done, DESUQ_OT_note, DESUQ_OT_who, DESUQ_MT_menu, DESUQ_Preferences)


def register():
    for cls in _classes:
        bpy.utils.register_class(cls)
    bpy.types.TOPBAR_MT_file.append(_menu)
    bpy.app.handlers.load_pre.append(_on_load_pre)
    bpy.app.handlers.load_post.append(_on_load_post)
    bpy.app.handlers.save_post.append(_on_save_post)
    atexit.register(_release_if_ours)


def unregister():
    atexit.unregister(_release_if_ours)
    for h, fn in ((bpy.app.handlers.load_pre, _on_load_pre),
                  (bpy.app.handlers.load_post, _on_load_post),
                  (bpy.app.handlers.save_post, _on_save_post)):
        if fn in h:
            h.remove(fn)
    bpy.types.TOPBAR_MT_file.remove(_menu)
    for cls in reversed(_classes):
        bpy.utils.unregister_class(cls)
