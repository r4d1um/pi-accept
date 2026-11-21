#!/usr/bin/env python3
# Renders a command's terminal output to a PNG (used by tools/screenshots.sh). Needs: pip install pyte pillow fonttools
# (wcwidth comes with pyte), DejaVu Sans Mono, optional Noto Color Emoji for ℹ and emoji
"""Run a command in a pseudo-terminal and save what the terminal shows as a PNG (dark terminal colours)."""
import os, pty, sys, time, select, struct, fcntl, termios
import pyte
from PIL import Image, ImageDraw, ImageFont
cols, rows, out = int(sys.argv[1]), int(sys.argv[2]), sys.argv[3]
cmd = sys.argv[4:]
pid, fd = pty.fork()
if pid == 0:
    fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
    os.environ.update(TERM="xterm-256color", COLUMNS=str(cols), LANG="C.UTF-8")
    os.execvp(cmd[0], cmd)
scr = pyte.Screen(cols, rows); st = pyte.ByteStream(scr)
end = time.time() + 20
while time.time() < end:
    r, _, _ = select.select([fd], [], [], 0.2)
    if r:
        try:
            d = os.read(fd, 65536)
        except OSError:
            break
        if not d:
            break
        st.feed(d)
BASE = {"black": "1d1f21", "red": "cc6666", "green": "b5bd68", "brown": "f0c674", "yellow": "f0c674", "blue": "81a2be",
        "magenta": "b294bb", "cyan": "8abeb7", "white": "c5c8c6"}
BRIGHT = {"black": "666666", "red": "ff7b7b", "green": "d0e090", "brown": "ffe08a", "yellow": "ffe08a", "blue": "a0c4ff",
          "magenta": "d4b0e0", "cyan": "a0e0e0", "white": "ffffff"}
FG, BG = "c5c8c6", "16181b"
F = "/usr/share/fonts/truetype/dejavu/DejaVuSansMono{}.ttf"
reg, bold = ImageFont.truetype(F.format(""), 14), ImageFont.truetype(F.format("-Bold"), 14)
CW, CH = 9, 18
# Glyphs DejaVu Sans Mono lacks (ℹ, emoji) come from Noto Color Emoji, drawn at its bitmap size and scaled down.
from wcwidth import wcwidth
from fontTools.ttLib import TTFont
MONO = TTFont(F.format("")).getBestCmap()
EMOJI_PATH = "/usr/share/fonts/truetype/noto/NotoColorEmoji.ttf"
emoji = ImageFont.truetype(EMOJI_PATH, 109) if os.path.exists(EMOJI_PATH) else None
_cache = {}
def fallback(chr_):
    if chr_ not in _cache:
        w = max(1, wcwidth(chr_)) * CW
        g = Image.new("RGBA", (136, 128), (0, 0, 0, 0))
        ImageDraw.Draw(g).text((0, 0), chr_, font=emoji, embedded_color=True)
        g = g.crop(g.getbbox() or (0, 0, 1, 1))
        k = min(w / g.width, (CH - 2) / g.height)
        _cache[chr_] = g.resize((max(1, int(g.width * k)), max(1, int(g.height * k))), Image.LANCZOS)
    return _cache[chr_]
used = max((y for y in range(rows) if "".join(scr.buffer[y][x].data for x in range(cols)).strip()), default=0) + 2
img = Image.new("RGB", (cols * CW + 16, used * CH + 16), "#" + BG)
dr = ImageDraw.Draw(img)
def col(c, default, bright=False):
    if c in ("default", None, ""):
        return default
    if c in BASE:
        return (BRIGHT if bright else BASE)[c]
    return c                                   # 256 colours / truecolour: pyte gives hex
for y in range(used):
    for x in range(cols):
        ch = scr.buffer[y][x]
        fg, bg = col(ch.fg, FG, ch.bold), col(ch.bg, BG)
        if ch.reverse:
            fg, bg = bg, fg
        px, py = 8 + x * CW, 8 + y * CH
        if bg != BG:
            dr.rectangle((px, py, px + CW - 1, py + CH - 1), fill="#" + bg)
        if ch.data.strip():
            if emoji and len(ch.data) == 1 and ord(ch.data) > 127 and ord(ch.data) not in MONO:
                g = fallback(ch.data)
                img.paste(g, (px, py + (CH - g.height) // 2), g)
            else:
                dr.text((px, py + 1), ch.data, font=bold if ch.bold else reg, fill="#" + fg)
img.save(out)
print(out, img.size)
