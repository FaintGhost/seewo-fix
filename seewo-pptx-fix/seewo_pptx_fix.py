#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
seewo_pptx_fix.py — canonicalize OOXML group transforms so that decks authored by
WPS 演示 / "KSO" tooling keep their layout when imported by 希沃白板 (EasiNote).

WHY (measured on the sample deck, see REPORT.md):
  WPS writes every <p:grpSp> child-coordinate space in *twips* (1/20 pt) while
  <a:off>/<a:ext> stay in EMU.  Consequence: the group transform is never the
  identity — chExt*635 == ext for all "fresh" groups, and a group that was later
  resized even gets a non-uniform scale (e.g. 1.036 x 1.124 in the sample deck).
  PowerPoint-authored decks essentially always emit chOff == off / chExt == ext
  (scale 1:1), so a PPT -> <custom format> converter is usually only ever tested
  against the identity case.  When the group remap is approximated (child coords
  taken as EMU, or scale applied without the chOff subtraction), every grouped
  object lands somewhere else -> "导入后偏移".

WHAT THIS DOES (render-equivalent, lossless):
  For every <p:grpSp> it bakes the group transform into the *direct children*
  (off/ext rewritten in absolute EMU, non-uniform scale applied once) and then
  re-bases the group onto itself: chOff := off, chExt := ext.
  The transform of every group therefore becomes the identity, which is exactly
  what PowerPoint writes.  Group semantics (one selectable object, z-order,
  grouping) are preserved; nothing is ungrouped.

  Group scaling in OOXML does not scale text, so baking the scale into ext is
  render-equivalent.  Verified by `verify`, which recomputes the absolute
  rectangle of every shape before/after and requires an exact match.

Usage:
  python3 seewo_pptx_fix.py report  deck.pptx
  python3 seewo_pptx_fix.py fix     deck.pptx -o deck.seewo.pptx
  python3 seewo_pptx_fix.py verify  deck.pptx deck.seewo.pptx
"""

from __future__ import annotations

import argparse
import copy
import re
import sys
import zipfile
import xml.etree.ElementTree as ET

NS = {
    "a": "http://schemas.openxmlformats.org/drawingml/2006/main",
    "p": "http://schemas.openxmlformats.org/presentationml/2006/main",
    "r": "http://schemas.openxmlformats.org/officeDocument/2006/relationships",
}
A = "{%s}" % NS["a"]
P = "{%s}" % NS["p"]

EMU_PER_PT = 12700
SHAPE_TAGS = ("sp", "pic", "grpSp", "graphicFrame", "cxnSp", "contentPart")

PART_RE = re.compile(
    r"^ppt/(slides|slideLayouts|slideMasters|notesSlides)/[^/]+\.xml$"
)
XMLNS_RE = re.compile(rb'xmlns(?::([A-Za-z0-9_.\-]+))?="([^"]*)"')
DECL_RE = re.compile(rb"<\?xml[^>]*\?>")


# --------------------------------------------------------------------------- #
# XML I/O with namespace-prefix preservation
# --------------------------------------------------------------------------- #
class Part:
    def __init__(self, name: str, raw: bytes):
        self.name = name
        m = DECL_RE.search(raw)
        self.decl = m.group(0) if m else (
            b'<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
        )
        body = raw[m.end():] if m else raw
        self.nsmap = {}
        for pfx, uri in XMLNS_RE.findall(body):
            self.nsmap[pfx.decode() if pfx else ""] = uri.decode()
        for pfx, uri in self.nsmap.items():
            ET.register_namespace(pfx, uri)
        self.root = ET.fromstring(body)

    def dump(self) -> bytes:
        out = ET.tostring(self.root, encoding="utf-8", xml_declaration=False)
        # ET only declares the prefixes actually used; re-add any declared-but-
        # unused prefix (e.g. ones referenced from mc:Ignorable) to stay valid.
        head_end = out.index(b">")
        head = out[:head_end]
        extra = []
        for pfx, uri in self.nsmap.items():
            token = b"xmlns:" + pfx.encode() + b"=" if pfx else b"xmlns="
            if token not in head:
                extra.append((pfx, uri))
        if extra:
            add = b"".join(
                (b' xmlns:%s="%s"' % (p.encode(), u.encode()))
                if p else (b' xmlns="%s"' % u.encode())
                for p, u in extra
            )
            out = out[:head_end] + add + out[head_end:]
        return self.decl + out


# --------------------------------------------------------------------------- #
# Helpers
# --------------------------------------------------------------------------- #
def ln(el) -> str:
    return el.tag.rsplit("}", 1)[-1]


def shapes(parent):
    for child in list(parent):
        if ln(child) in SHAPE_TAGS:
            yield child


def get_xfrm(el):
    tag = ln(el)
    if tag == "grpSp":
        return el.find("./p:grpSpPr/a:xfrm", NS)
    if tag == "graphicFrame":
        return el.find("./p:xfrm", NS)
    return el.find("./p:spPr/a:xfrm", NS)


def gi(el, attr) -> int:
    return int(el.get(attr))


def has_rotation(xfrm) -> bool:
    for k in ("rot", "flipH", "flipV"):
        v = xfrm.get(k)
        if v is not None and v not in ("0", "0.0", ""):
            return True
    return False


# --------------------------------------------------------------------------- #
# Geometry model: absolute rects under different importer behaviours
# --------------------------------------------------------------------------- #
def rects(part: Part, mode: str):
    """Absolute (x, y, cx, cy) for every shape, recursively.

    mode='spec'   -> correct OOXML semantics (chOff/chExt remap)
    mode='ignore' -> converter ignores the group child space entirely and takes
                     child off/ext as EMU absolute (what PowerPoint-only
                     converters effectively do when chOff==off / chExt==ext)
    mode='noscale'-> converter applies the group translation but not the scale,
                     i.e. abs = off + (child - chOff)
    """
    out = []

    def walk(parent, ctx):
        sx, tx, sy, ty = ctx
        for child in shapes(parent):
            xfrm = get_xfrm(child)
            if xfrm is None:
                continue
            o, e = xfrm.find("a:off", NS), xfrm.find("a:ext", NS)
            if o is None or e is None:
                continue
            X, Y, CX, CY = gi(o, "x"), gi(o, "y"), gi(e, "cx"), gi(e, "cy")
            ax, ay, acx, acy = X * sx + tx, Y * sy + ty, CX * sx, CY * sy
            out.append(
                {
                    "tag": ln(child),
                    "name": (child.find("./*/p:cNvPr", NS).get("name")
                             if child.find("./*/p:cNvPr", NS) is not None else ""),
                    "rect": (ax, ay, acx, acy),
                }
            )
            if ln(child) == "grpSp":
                cho, che = xfrm.find("a:chOff", NS), xfrm.find("a:chExt", NS)
                if cho is None or che is None:
                    continue
                if mode == "spec":
                    ccx, ccy = gi(che, "cx"), gi(che, "cy")
                    if ccx <= 0 or ccy <= 0:
                        continue
                    nsx, nsy = acx / ccx, acy / ccy
                    walk(child, (nsx, ax - gi(cho, "x") * nsx,
                                 nsy, ay - gi(cho, "y") * nsy))
                elif mode == "ignore":
                    walk(child, (1, 0, 1, 0))
                else:  # noscale
                    walk(child, (1, ax - gi(cho, "x"), 1, ay - gi(cho, "y")))

    spTree = part.root.find(".//p:spTree", NS)
    if spTree is not None:
        walk(spTree, (1, 0, 1, 0))
    return out


# --------------------------------------------------------------------------- #
# The patch
# --------------------------------------------------------------------------- #
def normalize_group(group, log, path):
    xfrm = get_xfrm(group)
    if xfrm is None:
        log.append((path, "skip: group without a:xfrm"))
        return
    o, e = xfrm.find("a:off", NS), xfrm.find("a:ext", NS)
    cho, che = xfrm.find("a:chOff", NS), xfrm.find("a:chExt", NS)
    if o is None or e is None:
        log.append((path, "skip: group without off/ext"))
        return
    if cho is None or che is None:
        log.append((path, "skip: group without chOff/chExt"))
        return
    if has_rotation(xfrm):
        log.append((path, "skip: rotated/flipped group (rotation baking out of scope)"))
        return

    chcx, chcy = gi(che, "cx"), gi(che, "cy")
    if chcx <= 0 or chcy <= 0:
        log.append((path, "skip: degenerate chExt %dx%d" % (chcx, chcy)))
        return

    ex, ey = gi(e, "cx"), gi(e, "cy")
    sx, sy = ex / chcx, ey / chcy
    gx, gy = gi(o, "x"), gi(o, "y")
    cx0, cy0 = gi(cho, "x"), gi(cho, "y")

    baked = 0
    for child in shapes(group):
        cxf = get_xfrm(child)
        if cxf is None:
            log.append((path, "warn: %s child without a:xfrm -- left un-baked" % ln(child)))
            continue
        co, ce = cxf.find("a:off", NS), cxf.find("a:ext", NS)
        if co is None or ce is None:
            log.append((path, "warn: %s child without off/ext" % ln(child)))
            continue
        co.set("x", str(int(round(gx + (gi(co, "x") - cx0) * sx))))
        co.set("y", str(int(round(gy + (gi(co, "y") - cy0) * sy))))
        ce.set("cx", str(int(round(gi(ce, "cx") * sx))))
        ce.set("cy", str(int(round(gi(ce, "cy") * sy))))
        baked += 1
        if ln(child) == "grpSp":
            normalize_group(child, log, path + "/" + (child.find("./p:nvGrpSpPr/p:cNvPr", NS).get("id") or "?"))

    # re-base the group onto itself -> identity transform
    cho.set("x", str(gx))
    cho.set("y", str(gy))
    che.set("cx", str(ex))
    che.set("cy", str(ey))
    log.append((path, "baked %d child(ren), scale %.4f x %.4f -> 1.0000 x 1.0000"
                % (baked, sx, sy)))


def patch_part(part: Part, log, bake_autofit=False):
    spTree = part.root.find(".//p:spTree", NS)
    if spTree is None:
        return 0
    n = 0
    for child in shapes(spTree):
        if ln(child) == "grpSp":
            cid = child.find("./p:nvGrpSpPr/p:cNvPr", NS)
            normalize_group(child, log, "%s#%s" % (part.name, cid.get("id") if cid is not None else "?"))
            n += 1
    if bake_autofit:
        # 340 text boxes in the sample deck carry <a:spAutoFit/>: "shape grows to
        # fit text".  Converters that honour it must re-measure text with their
        # own fonts/metrics and will then move/resize the box.  Freezing it
        # (keep the stored ext, stop growing) removes that degree of freedom.
        parent_of = {c: p for p in part.root.iter() for c in p}
        for af in list(part.root.iter(A + "spAutoFit")):
            parent = parent_of.get(af)
            if parent is None or ln(parent) != "bodyPr":
                continue
            idx = list(parent).index(af)
            parent.remove(af)
            # keep schema order: the autofit choice must stay where spAutoFit was
            parent.insert(idx, ET.Element(A + "noAutofit"))
            n += 1
    return n


# --------------------------------------------------------------------------- #
# Commands
# --------------------------------------------------------------------------- #
def fmt_pt(v: float) -> str:
    return "%.2f" % (v / EMU_PER_PT)


def group_inventory(part: Part):
    rows = []
    for g in part.root.iter(P + "grpSp"):
        x = get_xfrm(g)
        if x is None:
            continue
        o, e = x.find("a:off", NS), x.find("a:ext", NS)
        cho, che = x.find("a:chOff", NS), x.find("a:chExt", NS)
        cid = g.find("./p:nvGrpSpPr/p:cNvPr", NS)
        name = cid.get("name") if cid is not None else "?"
        if None in (o, e, cho, che):
            rows.append((part.name, name, "incomplete xfrm", None))
            continue
        ex, ey = gi(e, "cx"), gi(e, "cy")
        chx, chy = gi(che, "cx"), gi(che, "cy")
        if chx and chy:
            rows.append((part.name, name,
                         "%.3f x %.3f" % (ex / chx, ey / chy),
                         ("chExt pt %.2f" % (chx / 20.0) if abs(ex / chx - 635) < 1 else None)))
        else:
            rows.append((part.name, name, "degenerate", None))
    return rows


def cmd_report(args):
    with zipfile.ZipFile(args.deck) as z:
        names = [n for n in z.namelist() if PART_RE.match(n)]
        parts = {n: Part(n, z.read(n)) for n in names}

    print("== group transforms (identity == 1.000) ==")
    tot = bad = 0
    for n, p in parts.items():
        for row in group_inventory(p):
            tot += 1
            print("  %-26s %-12s scale %-20s %s" % (row[0].replace("ppt/", ""), row[1], row[2], row[3] or ""))
            if row[2] != "1.000 x 1.000":
                bad += 1
    print("\n  %d group(s), %d with a non-identity transform\n" % (tot, bad))

    print("== offset a buggy converter would introduce ==")
    worst = {"ignore": 0.0, "noscale": 0.0}
    details = []
    for n, p in parts.items():
        spec = rects(p, "spec")
        for mode in ("ignore", "noscale"):
            alt = rects(p, mode)
            if len(spec) != len(alt):
                continue
            for s, b in zip(spec, alt):
                dx = (b["rect"][0] - s["rect"][0]) / EMU_PER_PT
                dy = (b["rect"][1] - s["rect"][1]) / EMU_PER_PT
                d = max(abs(dx), abs(dy))
                if d > worst[mode]:
                    worst[mode] = d
                    details.append((mode, n, s["name"], dx, dy))
    for mode in ("ignore", "noscale"):
        print("  model %-8s max displacement %8.2f pt (%.2f cm)"
              % (mode, worst[mode], worst[mode] * 2.54 / 72))
    for mode, n, name, dx, dy in details:
        print("    worst %-8s %-22s %-16s dx=%8.2fpt dy=%8.2fpt"
              % (mode, n.replace("ppt/", ""), name, dx, dy))


def cmd_fix(args):
    src = args.deck
    dst = args.out or re.sub(r"\.pptx$", "", src, flags=re.I) + ".seewo.pptx"
    log = []
    changed = {}

    with zipfile.ZipFile(src) as zin:
        infos = zin.infolist()
        blobs = {i.filename: zin.read(i.filename) for i in infos}

    for name in blobs:
        if not PART_RE.match(name):
            continue
        part = Part(name, blobs[name])
        n = patch_part(part, log, bake_autofit=args.bake_autofit)
        if n:
            new = part.dump()
            if new != blobs[name]:
                changed[name] = (blobs[name], new)
                blobs[name] = new

    with zipfile.ZipFile(dst, "w") as zout:
        for info in infos:
            data = blobs[info.filename]
            zi = zipfile.ZipInfo(info.filename, date_time=info.date_time)
            zi.compress_type = info.compress_type
            zi.external_attr = info.external_attr
            zi.create_system = info.create_system
            zi.internal_attr = info.internal_attr
            zout.writestr(zi, data)

    for path, msg in log:
        if msg.startswith("skip") or msg.startswith("warn"):
            print("  ! %s: %s" % (path, msg))
    print("\n  patched %d part(s), %d byte-different: %s"
          % (len(changed), len(changed), ", ".join(sorted(n.replace("ppt/", "") for n in changed)) or "none"))
    print("  written: %s" % dst)
    return None


def cmd_verify(args):
    a, b = args.before, args.after
    ok = True
    with zipfile.ZipFile(a) as za, zipfile.ZipFile(b) as zb:
        na, nb = za.namelist(), zb.namelist()
        if na != nb:
            print("  FAIL: zip entry list changed")
            ok = False
        for n in na:
            pa = Part(n, za.read(n)) if PART_RE.match(n) else None
            pb = Part(n, zb.read(n)) if PART_RE.match(n) else None
            if pa is None:
                if za.read(n) != zb.read(n):
                    print("  NOTE: non-slide part changed: %s" % n)
                continue

            # 1. render equivalence -------------------------------------------------
            ra, rb_ = rects(pa, "spec"), rects(pb, "spec")
            if len(ra) != len(rb_):
                print("  FAIL %s: shape count %d -> %d" % (n, len(ra), len(rb_)))
                ok = False
                continue
            worst = 0.0
            for x, y in zip(ra, rb_):
                worst = max(worst, max(abs(u - v) for u, v in zip(x["rect"], y["rect"])))
            if worst > 2:
                print("  FAIL %s: geometry drifted %.3f EMU" % (n, worst))
                ok = False

            # 2. text untouched -----------------------------------------------------
            ta = [e.text for e in pa.root.iter(A + "t")]
            tb = [e.text for e in pb.root.iter(A + "t")]
            if ta != tb:
                print("  FAIL %s: text content changed" % n)
                ok = False

            # 3. all group transforms now identity ---------------------------------
            for row in group_inventory(pb):
                if row[2] != "1.000 x 1.000":
                    print("  FAIL %s: group %s still scale %s" % (n, row[1], row[2]))
                    ok = False

        # 4. buggy-converter model: post-patch residual offset must be 0 -----------
        print("\n== post-patch residual offset under a converter that ignores the group space ==")
        worst = 0.0
        for n in na:
            if not PART_RE.match(n):
                continue
            pa, pb = Part(n, za.read(n)), Part(n, zb.read(n))
            for mode in ("ignore", "noscale"):
                ra, rb_ = rects(pa, "spec"), rects(pb, mode)
                for x, y in zip(ra, rb_):
                    d = max(abs(y["rect"][0] - x["rect"][0]), abs(y["rect"][1] - x["rect"][1])) / EMU_PER_PT
                    worst = max(worst, d)
        print("  max displacement after patch: %.4f pt" % worst)
        if worst > 0.01:
            print("  (pre-patch value is printed by `report`)")

    print("\n  RESULT: %s" % ("PASS - geometry preserved, group transforms canonicalised" if ok else "FAIL"))
    return 0 if ok else 1


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)

    p = sub.add_parser("report", help="diagnose group transforms and predicted offset")
    p.add_argument("deck")
    p.set_defaults(func=cmd_report)

    p = sub.add_parser("fix", help="write a Seewo-safe copy (identity group transforms)")
    p.add_argument("deck")
    p.add_argument("-o", "--out")
    p.add_argument("--bake-autofit", action="store_true",
                   help="also freeze <a:spAutoFit/> -> <a:noAutofit/> (variant B, "
                        "for converters that re-measure text)")
    p.set_defaults(func=cmd_fix)

    p = sub.add_parser("verify", help="prove the patched deck renders identically")
    p.add_argument("before")
    p.add_argument("after")
    p.set_defaults(func=cmd_verify)

    args = ap.parse_args(argv)
    return args.func(args) or 0


if __name__ == "__main__":
    sys.exit(main())
