#!/usr/bin/env python3
"""Генерирует icon.ico для mbtorrent (нужен Pillow)."""
from PIL import Image, ImageDraw

S = 4                 # суперсэмплинг
N = 256 * S
BG_TOP = (14, 20, 32)
BG_BOTTOM = (8, 11, 17)
C1 = (94, 234, 212)   # #5eead4
C2 = (56, 189, 248)   # #38bdf8


def lerp(a, b, t):
    return tuple(round(x + (y - x) * t) for x, y in zip(a, b))


def gradient(size):
    img = Image.new("RGB", (size, size))
    px = img.load()
    for y in range(size):
        for x in range(size):
            t = (x + y) / (2 * size - 2)
            px[x, y] = lerp(C1, C2, t)
    return img


def main():
    bg = Image.new("RGB", (N, N))
    d = ImageDraw.Draw(bg)
    for y in range(N):
        d.line([(0, y), (N, y)], fill=lerp(BG_TOP, BG_BOTTOM, y / (N - 1)))

    # скруглённый квадрат как маска фона
    radius = int(56 * S)
    shape = Image.new("L", (N, N), 0)
    ImageDraw.Draw(shape).rounded_rectangle([0, 0, N - 1, N - 1], radius=radius, fill=255)
    canvas = Image.new("RGB", (N, N), (0, 0, 0))
    canvas.paste(bg, (0, 0), shape)

    # волна «m» из логотипа
    pts = [(72, 180), (72, 104), (100, 136), (128, 104), (156, 136), (184, 104), (184, 180)]
    pts = [(x * S, y * S) for x, y in pts]
    width = 20 * S

    stroke = Image.new("L", (N, N), 0)
    sd = ImageDraw.Draw(stroke)
    sd.line(pts, fill=255, width=width, joint="curve")
    r = width // 2
    for x, y in (pts[0], pts[-1]):
        sd.ellipse([x - r, y - r, x + r, y + r], fill=255)

    canvas.paste(gradient(N), (0, 0), stroke)

    # мягкий ободок
    ring = Image.new("L", (N, N), 0)
    ImageDraw.Draw(ring).rounded_rectangle(
        [1, 1, N - 2, N - 2], radius=radius, outline=90, width=max(1, S)
    )
    canvas.paste(Image.new("RGB", (N, N), (120, 200, 230)), (0, 0), ring)

    sizes = [256, 128, 64, 48, 32, 24, 16]
    frames = [canvas.resize((s, s), Image.LANCZOS) for s in sizes]
    frames[0].save("icon.ico", sizes=[(s, s) for s in sizes], append_images=frames[1:])
    print("icon.ico готов")


if __name__ == "__main__":
    main()
