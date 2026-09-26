# desuqcafe Syncthing -- the add-on's conversation with the running Syncthing.
#
# Nothing in this file imports bpy, so it can be tested with any Python
# (custom/scripts/test-blender-addon.py) as well as inside Blender.
#
# It talks to the same REST API the main screen and the tray use, on this
# computer only, with the API key from this computer's own config.xml. It
# never starts Syncthing and never changes a setting: it marks files, takes
# marks off, writes notes, and asks questions.

import json
import os
import ssl
import urllib.error
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET

# Where the installer puts this computer's Syncthing home. DESUQ_HOME
# overrides it, which is how the tests point it at a throwaway instance.
DATA_DIR_NAME = "desuqcafe-syncthing"

# Short on purpose: these calls happen while a file is opening, and a
# Syncthing that is not running answers "connection refused" at once. Only a
# hung one would wait, and Blender should not wait long for that.
TIMEOUT = 3.0


class Unavailable(Exception):
    """Syncthing is not set up here, not running, or refused the request.
    The message is a sentence for a person."""


def default_home():
    env = os.environ.get("DESUQ_HOME")
    if env:
        return env
    base = os.environ.get("LOCALAPPDATA") or os.path.expanduser("~")
    return os.path.join(base, DATA_DIR_NAME)


class Endpoint:
    def __init__(self, base_url, api_key, ssl_context=None):
        self.base_url = base_url
        self.api_key = api_key
        self.ssl_context = ssl_context


def read_endpoint(home):
    """The GUI address and API key from config.xml, the way the tray reads
    them (custom/tray/client.go, readEndpoint)."""
    path = os.path.join(home, "config.xml")
    try:
        root = ET.parse(path).getroot()
    except (OSError, ET.ParseError):
        raise Unavailable("desuqcafe Syncthing is not set up on this computer.")
    gui = root.find("gui")
    if gui is None:
        raise Unavailable("desuqcafe Syncthing's settings have no web interface address.")
    address = (gui.findtext("address") or "").strip()
    api_key = (gui.findtext("apikey") or "").strip()
    if not address or not api_key:
        raise Unavailable("desuqcafe Syncthing's settings have no web interface address.")

    # A GUI bound to every interface is still reached on loopback.
    host, _, port = address.rpartition(":")
    if host in ("", "0.0.0.0", "[::]", "::"):
        address = "127.0.0.1:" + port

    ctx = None
    scheme = "http"
    if gui.get("tls") == "true":
        scheme = "https"
        ctx = _pinned_context(home)
    return Endpoint(scheme + "://" + address, api_key, ctx)


def _pinned_context(home):
    # Syncthing's GUI certificate names the device, not 127.0.0.1, so no
    # hostname check can pass (CLAUDE.md). Trust exactly that one
    # certificate instead, the way the tray does (custom/tray/tlspin.go).
    cert = os.path.join(home, "https-cert.pem")
    try:
        ctx = ssl.create_default_context(cafile=cert)
    except (OSError, ssl.SSLError):
        raise Unavailable("desuqcafe Syncthing's certificate could not be read.")
    ctx.check_hostname = False
    if hasattr(ssl, "VERIFY_X509_PARTIAL_CHAIN"):
        ctx.verify_flags |= ssl.VERIFY_X509_PARTIAL_CHAIN
    return ctx


class Client:
    def __init__(self, endpoint):
        self.ep = endpoint

    @classmethod
    def local(cls, home=None):
        return cls(read_endpoint(home or default_home()))

    def _request(self, method, path, query=None, body=None):
        url = self.ep.base_url + path
        if query:
            url += "?" + urllib.parse.urlencode(query)
        data = None
        headers = {"X-API-Key": self.ep.api_key}
        if body is not None:
            data = json.dumps(body).encode("utf-8")
            headers["Content-Type"] = "application/json"
        req = urllib.request.Request(url, data=data, headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=TIMEOUT, context=self.ep.ssl_context) as resp:
                raw = resp.read()
        except urllib.error.HTTPError as e:
            # The fork's handlers answer a refusal with a sentence, which is
            # worth passing on as it is: "the latest version of this file was
            # saved by somebody else" is something to act on.
            text = e.read().decode("utf-8", "replace").strip()
            raise Unavailable(_sentence(text) or "desuqcafe Syncthing refused that (%d)." % e.code)
        except (urllib.error.URLError, OSError):
            raise Unavailable("desuqcafe Syncthing is not running. Start it from the Start menu.")
        return json.loads(raw.decode("utf-8")) if raw else None

    def get(self, path, **query):
        return self._request("GET", path, query=query)

    def post(self, path, body):
        return self._request("POST", path, body=body)

    # ---------------------------------------------------------------- lookups

    def locate(self, file_path):
        """(folder id, folder label, slash-separated path inside it) for a
        file on disk, or None when no synced folder holds it."""
        folders = self.get("/rest/config/folders") or []
        return folder_for_path(folders, file_path)

    def claims(self, folder):
        return self.get("/rest/folder/claims", folder=folder) or {}

    def who_has(self, folder, rel):
        return self.get("/rest/db/whohas", folder=folder, file=rel) or {}

    def notes(self, folder, rel):
        return (self.get("/rest/folder/notes", folder=folder, file=rel) or {}).get("notes") or []

    # ---------------------------------------------------------------- changes

    def mark(self, folder, rel, auto=False):
        return self.post("/rest/folder/claim", {"folder": folder, "path": rel, "auto": auto})

    def release(self, folder, rel):
        return self.post("/rest/folder/claim", {"folder": folder, "path": rel, "release": True})

    def note(self, folder, rel, text):
        return self.post("/rest/folder/note", {"folder": folder, "path": rel, "text": text})


def _sentence(text):
    text = (text or "").strip()
    if not text:
        return ""
    text = text[0].upper() + text[1:]
    return text if text.endswith(".") else text + "."


def _norm(p):
    return os.path.normcase(os.path.normpath(os.path.abspath(p)))


def folder_for_path(folders, file_path):
    """The innermost synced folder holding file_path. Pure, for the tests."""
    target = _norm(file_path)
    best = None
    for f in folders:
        root = f.get("path") or ""
        if not root:
            continue
        root_n = _norm(os.path.expanduser(root))
        if target == root_n or not target.startswith(root_n.rstrip(os.sep) + os.sep):
            continue
        if best is None or len(root_n) > len(best[0]):
            best = (root_n, f)
    if best is None:
        return None
    f = best[1]
    # Measured on the paths as given rather than the normcased ones, so the
    # relative part keeps the case it has on disk -- which is what the index
    # holds. normcase above was only for comparing.
    root = os.path.normpath(os.path.abspath(os.path.expanduser(f["path"])))
    rel = os.path.normpath(os.path.abspath(file_path))[len(root.rstrip(os.sep)) + 1:].replace(os.sep, "/")
    return f["id"], (f.get("label") or f["id"]), rel


# -------------------------------------------------------------------- words
#
# What the add-on says when a file opens. Pure, so the tests can read it.

def others_marks(claims_reply, rel):
    return [c for c in (claims_reply.get("claims") or [])
            if c.get("path") == rel and not c.get("mine")]


def my_mark(claims_reply, rel):
    for c in claims_reply.get("claims") or []:
        if c.get("path") == rel and c.get("mine"):
            return c
    return None


def latest_note(notes):
    """The newest note about the version in the folder now, or None."""
    current = [n for n in notes if n.get("current")]
    current.sort(key=lambda n: n.get("at") or "", reverse=True)
    return current[0] if current else None


def opening_warnings(name, claims_reply, who, notes, rel):
    """The lines to show when a file opens. Empty means nothing worth
    interrupting anybody for."""
    lines = []
    for c in others_marks(claims_reply, rel):
        if c.get("handingTo") and c["handingTo"].get("mine"):
            lines.append("%s is handing %s to you." % (c.get("name"), name))
            continue
        lines.append("%s is working on %s." % (c.get("name"), name))
        lines.append("If you both save it, one of you ends up with a copy of your own.")
    if who.get("behind"):
        by = who.get("modifiedBy") or "somebody"
        lines.append("A newer version by %s is still on its way to this computer." % by)
        lines.append("Wait for it before changing anything, or you will be editing an old copy.")
    n = latest_note(notes)
    if n and not n.get("mine"):
        lines.append('%s, about this version: "%s"' % (n.get("name"), n.get("text")))
    return lines


def who_has_text(name, claims_reply, who, notes, rel):
    """Who has this? as a few short lines."""
    lines = []
    marks = [c for c in (claims_reply.get("claims") or []) if c.get("path") == rel]
    for c in marks:
        lines.append("%s working on it." % ("You are" if c.get("mine") else c.get("name") + " is"))
    if not marks:
        lines.append("Nobody has marked it as being worked on.")
    if not who.get("exists", True):
        lines.append("It has been deleted.")
        return lines
    if who.get("behind"):
        lines.append("A newer version by %s is on its way to you." % (who.get("modifiedBy") or "somebody"))
    has, missing, offline, held = [], [], [], []
    for p in who.get("peers") or []:
        if p.get("heldBack"):
            held.append(p.get("name"))
        elif p.get("has"):
            has.append(p.get("name"))
        elif not p.get("connected"):
            offline.append(p.get("name"))
        else:
            missing.append(p.get("name"))
    if not who.get("behind"):
        if has:
            lines.append("Has your version: " + ", ".join(has) + ".")
        if missing:
            lines.append("Still receiving it: " + ", ".join(missing) + ".")
        if offline:
            lines.append("Offline, without your version yet: " + ", ".join(offline) + ".")
    if held:
        lines.append("Not keeping this file: " + ", ".join(held) + ".")
    n = latest_note(notes)
    if n:
        lines.append('Why (%s): "%s"' % ("you" if n.get("mine") else n.get("name"), n.get("text")))
    return lines
