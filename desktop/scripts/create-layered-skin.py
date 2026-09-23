"""Generate the editable layered Mux example using only Python's standard library."""
from pathlib import Path
import json, struct, zlib, zipfile, math

OUT = Path(__file__).resolve().parents[1] / 'frontend/public/skins'
SOURCE = OUT / 'mux-layered-source'
SOURCE.mkdir(parents=True, exist_ok=True)
W = H = 256

def png(width, height, shapes):
    pixels = bytearray()
    for y in range(height):
        pixels.append(0)
        for x in range(width):
            samples = []
            for dx, dy in ((.25,.25),(.75,.25),(.25,.75),(.75,.75)):
                color = (0,0,0,0)
                for contains, fill in shapes:
                    if contains(x+dx,y+dy): color = fill
                samples.append(color)
            a = sum(c[3] for c in samples)
            pixels.extend([round(sum(c[k]*c[3] for c in samples)/a) if a else 0 for k in range(3)] + [round(a/4)])
    def chunk(kind, data):
        return struct.pack('>I',len(data))+kind+data+struct.pack('>I',zlib.crc32(kind+data)&0xffffffff)
    return b'\x89PNG\r\n\x1a\n'+chunk(b'IHDR',struct.pack('>IIBBBBB',width,height,8,6,0,0,0))+chunk(b'IDAT',zlib.compress(bytes(pixels)))+chunk(b'IEND',b'')

def ellipse(cx,cy,rx,ry): return lambda x,y: ((x-cx)/rx)**2+((y-cy)/ry)**2<=1

def rect(x1,y1,x2,y2): return lambda x,y: x1<=x<=x2 and y1<=y<=y2

def triangle(a,b,c):
    def inside(x,y):
        values=[(x-p[0])*(q[1]-p[1])-(y-p[1])*(q[0]-p[0]) for p,q in ((a,b),(b,c),(c,a))]
        return all(v>=0 for v in values) or all(v<=0 for v in values)
    return inside

purple=(130,111,219,255); edge=(105,91,183,255); shell=(221,218,252,255); face=(59,63,104,255); cyan=(173,244,248,255)
body=[(triangle((56,110),(60,31),(108,80)),edge),(triangle((200,110),(196,31),(148,80)),edge),
      (triangle((62,92),(66,45),(100,82)),purple),(triangle((194,92),(190,45),(156,82)),purple),
      (ellipse(128,137,85,78),edge),(ellipse(128,135,80,74),shell),
      (ellipse(128,145,64,36),face),(rect(88,112,168,172),face),
      (ellipse(95,89,18,5),(248,247,255,230)),(ellipse(81,157,8,4),(235,153,187,255)),(ellipse(175,157,8,4),(235,153,187,255))]
eyes=[(rect(6,4,12,18),cyan),(ellipse(9,4,3,3),cyan),(ellipse(9,18,3,3),cyan),
      (rect(58,4,64,18),cyan),(ellipse(61,4,3,3),cyan),(ellipse(61,18,3,3),cyan)]
mouth=[(lambda x,y: 5<=x<=27 and abs(y-(5+5*math.sin((x-5)/22*math.pi)))<=1.8,cyan)]
node=[(ellipse(10,10,8,8),edge),(ellipse(10,10,5,5),(154,237,249,255))]
assets={'body.png':png(256,256,body),'eyes.png':png(70,24,eyes),'mouth.png':png(32,16,mouth),'node.png':png(20,20,node)}
# Composite the same vector primitives for the static library thumbnail.
def shifted(shapes,ox,oy): return [(lambda x,y,f=f: f(x-ox,y-oy),color) for f,color in shapes]
assets['preview.png']=png(256,256,body+shifted(eyes,93,129)+shifted(mouth,112,156)+shifted(node,118,74))
manifest={'schemaVersion':3,'id':'org.hypomux.layered','name':'小 Mux · 分层动态','author':'HypoMux','version':'1.0.0',
 'canvas':{'width':256,'height':256},'anchor':{'x':.5,'y':.84},'preview':'preview.png','states':{'idle':{'type':'image','src':'preview.png'}},
 'layered':{'layers':[
  {'id':'body','src':'body.png','role':'body'},
  {'id':'eyes','src':'eyes.png','role':'eyes','x':93/256,'y':129/256,'width':70/256,'height':24/256},
  {'id':'mouth','src':'mouth.png','role':'mouth','x':112/256,'y':156/256,'width':32/256,'height':16/256},
  {'id':'node','src':'node.png','role':'decoration','x':118/256,'y':74/256,'width':20/256,'height':20/256,
   'motions':{'thinking':{'duration':1200,'loop':True,'frames':[{'opacity':1},{'opacity':.3},{'opacity':1}]},
              'hover':{'duration':600,'loop':True,'frames':[{'y':0},{'y':-.3},{'y':0}]}}}
 ]}}
assets['manifest.json']=json.dumps(manifest,ensure_ascii=False,indent=2).encode('utf-8')
for name,data in assets.items(): (SOURCE/name).write_bytes(data)
with zipfile.ZipFile(OUT/'mux-layered.muxskin','w',zipfile.ZIP_DEFLATED) as archive:
    for name,data in assets.items(): archive.writestr(name,data)
print(OUT/'mux-layered.muxskin')
