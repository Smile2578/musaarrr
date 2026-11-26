# The shop's furniture and hi-fi, modelled here and written to one .glb: the counter, a turntable
# (its platter apart, as it turns), an amplifier with lit VU meters, speakers and their stands, and the
# lamps (brass bars of opal globes or enamel shades over the bins, a wall sconce, a prismatic pendant,
# a floor lamp).
# Each model is one object, one primitive per material; the ambient occlusion is baked by Cycles into
# its vertex colours, which three multiplies into the colour. Materials are named: shop3d/world.js
# puts the oak picture on "walnut".
# usage: python tools/blender/hifi.py web/shop3d/assets/hifi/hifi.glb   (pip install bpy, Python 3.11)
import math
import sys

import bpy  # first: bmesh and mathutils come with it
import bmesh
from mathutils import Matrix

OUT = sys.argv[-1]
bpy.ops.wm.read_factory_settings(use_empty=True)
MATS = {}


def lin(hexstr):
    c = [int(hexstr[i:i + 2], 16) / 255 for i in (0, 2, 4)]
    return [x / 12.92 if x <= 0.04045 else ((x + 0.055) / 1.055) ** 2.4 for x in c]


def mat(name, color, rough=0.5, metal=0.0, emit=None, strength=0.0):
    if name not in MATS:
        m = bpy.data.materials.new(name)
        b = m.node_tree.nodes["Principled BSDF"]
        b.inputs["Base Color"].default_value = (*lin(color), 1)
        b.inputs["Roughness"].default_value = rough
        b.inputs["Metallic"].default_value = metal
        if emit:
            b.inputs["Emission Color"].default_value = (*lin(emit), 1)
            b.inputs["Emission Strength"].default_value = strength
        MATS[name] = m
    return MATS[name]


WALNUT = lambda: mat("walnut", "6b4630", 0.55)
BLACK = lambda: mat("black", "141414", 0.45, 0.2)
ALU = lambda: mat("alu", "c9c9c6", 0.32, 1.0)
BRASS = lambda: mat("brass", "b8913f", 0.3, 1.0)
RUBBER = lambda: mat("rubber", "0b0b0b", 0.9)
OPAL = lambda: mat("opal", "f6efe2", 0.25, 0.0, "ffc98a", 6.0)
GREEN = lambda: mat("paint_green", "1e3b2f", 0.5)


def part(bm, m, smooth=True, angle=35):
    """an object from bm, smooth but for its sharp edges, its uvs projected on a box in metres"""
    bm.normal_update()
    uv = bm.loops.layers.uv.new("UVMap")
    for f in bm.faces:
        f.smooth = smooth
        n = [abs(c) for c in f.normal]
        a, b = (1, 2) if n[0] >= max(n[1], n[2]) else (0, 2) if n[1] >= n[2] else (0, 1)
        for lp in f.loops:
            lp[uv].uv = (lp.vert.co[a], lp.vert.co[b])
    for e in bm.edges:
        if e.is_manifold and e.calc_face_angle(0) > math.radians(angle):
            e.smooth = False
    me = bpy.data.meshes.new("part")
    bm.to_mesh(me)
    bm.free()
    me.materials.append(m)
    o = bpy.data.objects.new("part", me)
    bpy.context.collection.objects.link(o)
    return o


def bevelled(bm, width, segs=2):
    """rounds the sharp edges only: a cylinder's sides stay as they are"""
    sharp = [e for e in bm.edges if e.is_manifold and e.calc_face_angle(0) > math.radians(30)]
    if width and sharp:
        bmesh.ops.bevel(bm, geom=sharp, offset=width, segments=segs, profile=0.5, affect="EDGES", clamp_overlap=True)


def box(size, at, m, bevel=0.003, segs=2):
    bm = bmesh.new()
    bmesh.ops.create_cube(bm, size=1.0)
    bmesh.ops.scale(bm, vec=size, verts=bm.verts)
    bevelled(bm, bevel, segs)
    bmesh.ops.translate(bm, vec=at, verts=bm.verts)
    return part(bm, m)


def cyl(r, h, at, m, segs=40, bevel=0.0, r2=None, axis="Z"):
    bm = bmesh.new()
    bmesh.ops.create_cone(bm, cap_ends=True, segments=segs, radius1=r, radius2=r if r2 is None else r2, depth=h)
    bevelled(bm, bevel, 2)
    if axis == "Y":
        bmesh.ops.rotate(bm, verts=bm.verts, cent=(0, 0, 0), matrix=Matrix.Rotation(math.pi / 2, 3, "X"))
    bmesh.ops.translate(bm, vec=at, verts=bm.verts)
    return part(bm, m)


def lathe(profile, at, m, segs=40, axis="Z", angle=70):
    """a solid of revolution of profile [(r, z)] around z, smooth but for its creases sharper than angle"""
    bm = bmesh.new()
    vs = [bm.verts.new((r, 0, z)) for r, z in profile]
    es = [bm.edges.new((vs[i], vs[i + 1])) for i in range(len(vs) - 1)]
    bmesh.ops.spin(bm, geom=vs + es, cent=(0, 0, 0), axis=(0, 0, 1), angle=2 * math.pi, steps=segs, use_duplicate=False)
    bmesh.ops.remove_doubles(bm, verts=bm.verts, dist=1e-6)
    bmesh.ops.recalc_face_normals(bm, faces=bm.faces)
    if axis == "Y":
        bmesh.ops.rotate(bm, verts=bm.verts, cent=(0, 0, 0), matrix=Matrix.Rotation(-math.pi / 2, 3, "X"))
    bmesh.ops.translate(bm, vec=at, verts=bm.verts)
    return part(bm, m, angle=angle)


def ball(R, zc, neck, n=14):
    """the profile of a glass ball of radius R centred at height zc, open at the top round a neck"""
    t0 = math.asin(neck / R)
    return [(max(R * math.sin(t), 0.0001), zc + R * math.cos(t)) for t in (t0 + (math.pi - t0) * i / n for i in range(n + 1))]


def tube(points, r, m):
    """a round bar along points"""
    cu = bpy.data.curves.new("bar", "CURVE")
    cu.dimensions = "3D"
    cu.bevel_depth = r
    cu.bevel_resolution = 2
    sp = cu.splines.new("POLY")
    sp.points.add(len(points) - 1)
    for p, xyz in zip(sp.points, points):
        p.co = (*xyz, 1)
    o = bpy.data.objects.new("bar", cu)
    bpy.context.collection.objects.link(o)
    me = bpy.data.meshes.new_from_object(o.evaluated_get(bpy.context.evaluated_depsgraph_get()))
    bpy.data.objects.remove(o)
    me.materials.clear()
    me.materials.append(m)
    ob = bpy.data.objects.new("bar", me)
    bpy.context.collection.objects.link(ob)
    for poly in me.polygons:
        poly.use_smooth = True
    return ob


def join(name, parts):
    for o in bpy.context.selected_objects:
        o.select_set(False)
    for o in parts:
        o.select_set(True)
    bpy.context.view_layer.objects.active = parts[0]
    with bpy.context.temp_override(active_object=parts[0], selected_editable_objects=parts, object=parts[0]):
        bpy.ops.object.join()
    o = parts[0]
    o.name = o.data.name = name
    return o


# ---------- the models (blender: z up, -y the front; the glTF export turns -y into three's +z) ----------

def counter():
    W, D, H = 3.6, 0.66, 0.98
    p = [box((W, D, H - 0.09), (0, 0, 0.09 + (H - 0.09) / 2), GREEN(), 0.006)]
    p.append(box((W - 0.04, D - 0.04, 0.09), (0, 0, 0.045), BLACK(), 0.004))  # the plinth, set back
    for i in range(5):  # raised panels on the front, a moulding round each
        x = -W / 2 + W / 5 * (i + 0.5)
        p.append(box((W / 5 - 0.16, 0.02, H - 0.36), (x, -D / 2 - 0.006, 0.09 + (H - 0.09) / 2), GREEN(), 0.008, 3))
        p.append(box((W / 5 - 0.1, 0.012, H - 0.3), (x, -D / 2 - 0.002, 0.09 + (H - 0.09) / 2), GREEN(), 0.004))
    p.append(box((W + 0.12, D + 0.1, 0.05), (0, 0, H + 0.025), WALNUT(), 0.012, 3))
    p.append(tube([(-W / 2 + 0.1, -D / 2 - 0.16, 0.2), (W / 2 - 0.1, -D / 2 - 0.16, 0.2)], 0.016, BRASS()))  # the foot rail
    for x in (-W / 2 + 0.12, 0, W / 2 - 0.12):  # the foot rail's brackets
        p.append(tube([(x, -D / 2 - 0.16, 0.2), (x, -D / 2 - 0.005, 0.2)], 0.008, BRASS()))
    return join("counter", p)


def turntable():
    p = [box((0.46, 0.36, 0.085), (0, 0, 0.0525), WALNUT(), 0.008, 3)]
    p.append(box((0.44, 0.34, 0.008), (0, 0, 0.098), ALU(), 0.002))
    for x in (-0.19, 0.19):
        for y in (-0.14, 0.14):
            p.append(cyl(0.022, 0.012, (x, y, 0.006), RUBBER(), 24, 0.003))
    p.append(cyl(0.024, 0.03, (0.165, 0.11, 0.117), ALU(), 32, 0.003))  # the arm's pillar
    p.append(cyl(0.012, 0.012, (0.165, 0.11, 0.138), BLACK(), 24, 0.002))
    p.append(tube([(0.165, 0.11, 0.142), (0.16, 0.02, 0.142), (0.12, -0.06, 0.142), (0.085, -0.1, 0.141)], 0.0045, ALU()))
    p.append(tube([(0.165, 0.11, 0.142), (0.167, 0.17, 0.142)], 0.0045, ALU()))
    p.append(cyl(0.017, 0.032, (0.167, 0.185, 0.142), ALU(), 32, 0.004, axis="Y"))  # the counterweight
    p.append(box((0.022, 0.034, 0.008), (0.078, -0.112, 0.136), BLACK(), 0.002))  # the headshell
    p.append(cyl(0.006, 0.03, (0.21, -0.07, 0.115), ALU(), 16))  # the arm rest
    for x in (-0.19, -0.15):
        p.append(cyl(0.011, 0.014, (x, -0.15, 0.108), ALU(), 24, 0.002))  # speed knobs
    deck = join("turntable", p)
    plat = [cyl(0.152, 0.022, (0, 0, 0.011), ALU(), 72, 0.002), cyl(0.147, 0.003, (0, 0, 0.0235), RUBBER(), 72)]
    plat.append(cyl(0.0035, 0.018, (0, 0, 0.03), ALU(), 16))
    platter = join("platter", plat)
    platter.location = (-0.05, 0, 0.102)
    platter.parent = deck
    return deck


def amplifier():
    W, D, H = 0.43, 0.3, 0.115
    p = [box((W - 0.03, D, H), (0, 0.004, H / 2 + 0.008), BLACK(), 0.004)]
    p.append(box((W - 0.03, 0.01, H), (0, -D / 2, H / 2 + 0.008), ALU(), 0.002))
    for s in (-1, 1):
        p.append(box((0.016, D + 0.01, H + 0.008), (s * (W / 2 - 0.008), 0.004, H / 2 + 0.008), WALNUT(), 0.004))
    for x in (-0.07, 0.07):
        p.append(box((0.094, 0.006, 0.054), (x, -D / 2 - 0.005, 0.088), BLACK(), 0.002))
        p.append(box((0.084, 0.004, 0.044), (x, -D / 2 - 0.008, 0.088), mat("vu", "f3d9a0", 0.6, 0.0, "ffb347", 2.2), 0))
    for x in (-0.16, -0.06, 0.06, 0.16):
        p.append(cyl(0.015, 0.022, (x, -D / 2 - 0.016, 0.034), ALU(), 32, 0.003, axis="Y"))
    for y in (-0.1, 0.1):
        for x in (-0.18, 0.18):
            p.append(cyl(0.012, 0.008, (x, y, 0.004), RUBBER(), 16))
    return join("amplifier", p)


def driver(x, z, r, y):
    """a loudspeaker driver on the baffle at y, all of it in front: no hole to cut. Its profile's
    depth is negative in front of the baffle: a metal frame, a rubber surround, a paper cone, a dust cap."""
    ring = lathe([(r * 1.1, 0), (r * 1.1, -0.004), (r, -0.004), (r * 0.92, -0.011), (r * 0.84, -0.007)], (0, 0, 0), BLACK(), 48, "Y")
    cone = lathe([(r * 0.84, -0.007), (r * 0.3, -0.002), (0.0001, -0.002)], (0, 0, 0), mat("cone", "1c1b19", 0.85), 48, "Y")
    cap = lathe([(r * 0.3, -0.002), (r * 0.2, -0.012), (0.0001, -0.015)], (0, 0, 0), mat("cone", "1c1b19", 0.85), 32, "Y")
    for o in (ring, cone, cap):
        o.location = (x, y, z)
    return [ring, cone, cap]


def speaker():
    W, D, H = 0.27, 0.25, 0.42
    p = [box((W, D, H), (0, 0, H / 2), WALNUT(), 0.01, 3)]
    p.append(box((W - 0.02, 0.006, H - 0.02), (0, -D / 2 - 0.001, H / 2), mat("baffle", "1d1a17", 0.7), 0.002))
    p += driver(0, 0.15, 0.095, -D / 2 - 0.004)  # the baffle's face
    p.append(lathe([(0.03, 0), (0.028, -0.003), (0.0001, -0.003)], (0, -D / 2 - 0.004, 0.335), BLACK(), 32, "Y"))
    p.append(lathe([(0.016, 0), (0.008, -0.01), (0.0001, -0.012)], (0, -D / 2 - 0.006, 0.335), mat("dome", "d8d3c8", 0.35, 0.3), 32, "Y"))
    return join("speaker", p)


def stand():
    p = [box((0.3, 0.28, 0.02), (0, 0, 0.01), BLACK(), 0.003), box((0.2, 0.18, 0.012), (0, 0, 0.594), BLACK(), 0.003)]
    for x in (-0.05, 0.05):
        p.append(box((0.04, 0.04, 0.58), (x, 0, 0.3), BLACK(), 0.004))
    return join("stand", p)


def rampe():
    """a brass bar of three opal globes, hung over the bins; its rods are drawn by the shop to the ceiling"""
    p = [box((1.5, 0.045, 0.035), (0, 0, 0), BRASS(), 0.006)]
    for x in (-0.55, 0, 0.55):
        p.append(cyl(0.028, 0.04, (x, 0, -0.035), BRASS(), 12))
        p.append(lathe(ball(0.085, -0.135, 0.026, 9), (x, 0, 0), OPAL(), 18))  # light: a big shop hangs hundreds
    return join("rampe", p)


def billard():
    """a brass bar of three enamel shades, green outside and white inside, hung over the bins"""
    p = [box((1.5, 0.045, 0.035), (0, 0, 0), BRASS(), 0.006)]
    cone = [(0.028, -0.02), (0.045, -0.05), (0.1, -0.11), (0.165, -0.165), (0.17, -0.172)]
    for x in (-0.55, 0, 0.55):
        p.append(cyl(0.012, 0.03, (x, 0, -0.02), BRASS(), 16))
        p.append(lathe(cone, (x, 0, 0), mat("enamel", "1e3b2f", 0.25, 0.1), 20))
        p.append(lathe([(r * 0.96, z - 0.004) for r, z in reversed(cone)], (x, 0, 0), mat("enamel_in", "f3eee2", 0.5, 0.0, "ffcf96", 0.6), 20))
        p.append(lathe(ball(0.04, -0.1, 0.012, 6), (x, 0, 0), mat("bulb", "fff1d6", 0.2, 0.0, "ffc27a", 8.0), 10))
    return join("billard", p)


def sconce():
    p = [cyl(0.05, 0.016, (0, 0.008, 0), BRASS(), 20, 0.003, axis="Y")]
    p.append(tube([(0, 0, 0), (0, -0.09, -0.01), (0, -0.16, 0.04), (0, -0.17, 0.09)], 0.007, BRASS()))
    tulip = [(0.012 + 0.056 * math.sin(math.pi / 2 * i / 8) ** 1.4, 0.085 + 0.12 * i / 8) for i in range(9)]
    p.append(lathe(tulip, (0, -0.17, 0), OPAL(), 16))
    return join("sconce", p)


def holophane():
    bell = [(0.025 + 0.18 * (i / 10) ** 1.6, -0.03 - 0.25 * i / 10) for i in range(11)] + [(0.205, -0.285), (0.12, -0.295), (0.0001, -0.297)]
    p = [lathe(bell, (0, 0, 0), mat("glass", "efe4cf", 0.15, 0.0, "ffc98a", 3.0), 28, angle=20)]  # its 28 sides stay sharp: the prisms
    p.append(lathe([(0.028, 0.02), (0.03, -0.03), (0.022, -0.035)], (0, 0, 0), BRASS(), 32))
    return join("holophane", p)


def floorlamp():
    p = [lathe([(0.16, 0), (0.16, 0.012), (0.12, 0.025), (0.02, 0.03)], (0, 0, 0), BRASS(), 48)]
    p.append(cyl(0.011, 1.42, (0, 0, 0.74), BRASS(), 16))
    p.append(lathe([(0.17, 1.38), (0.2, 1.38), (0.14, 1.62), (0.12, 1.62)], (0, 0, 0), mat("shade", "e8d3a8", 0.9, 0.0, "ffb066", 1.6), 48))
    return join("floorlamp", p)


MODELS = [counter, turntable, amplifier, speaker, stand, rampe, billard, sconce, holophane, floorlamp]
GROUNDED = {"counter", "turntable", "amplifier", "speaker", "stand", "floorlamp"}


def bake_ao(objects):
    """the occlusion of each model by itself (and the floor it stands on) into its vertex colours"""
    sc = bpy.context.scene
    sc.render.engine = "CYCLES"
    sc.cycles.device = "CPU"
    sc.cycles.samples = 48
    sc.render.bake.target = "VERTEX_COLORS"
    sc.world = bpy.data.worlds.new("world")
    sc.world.light_settings.distance = 0.2  # contact shadows only: the floor beyond lights the room too
    for i, o in enumerate(objects):
        o.location.x = i * 10  # alone in its corner of the world
    for o in objects:
        ground = None
        if o.name in GROUNDED:
            bpy.ops.mesh.primitive_plane_add(size=4, location=(o.location.x, 0, 0))
            ground = bpy.context.active_object
        todo = [o, *o.children]
        for ob in todo:
            ca = ob.data.color_attributes.new("ao", "FLOAT_COLOR", "CORNER")
            ob.data.color_attributes.active_color = ca
            for x in bpy.context.selected_objects:
                x.select_set(False)
            ob.select_set(True)
            bpy.context.view_layer.objects.active = ob
            bpy.ops.object.bake(type="AO", target="VERTEX_COLORS")
            for d in ca.data:  # softened: the room's light reaches into the corners too
                a = d.color[0]
                v = 0.35 + 0.65 * a
                d.color = (v, v, v, 1)
        if ground:
            bpy.data.objects.remove(ground)
    for o in objects:
        o.location.x = 0


objs = [m() for m in MODELS]
bake_ao(objs)
for o in bpy.context.selected_objects:
    o.select_set(False)
for o in objs:
    o.select_set(True)
    for ch in o.children:
        ch.select_set(True)
bpy.ops.export_scene.gltf(filepath=OUT, export_format="GLB", use_selection=True, export_apply=True, export_vertex_color="ACTIVE", export_yup=True, export_texcoords=True, export_normals=True)
print("written", OUT)
