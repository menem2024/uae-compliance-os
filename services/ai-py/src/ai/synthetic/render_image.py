"""Scanned-image variant of a PDF (spec section 5.5): 150 dpi, seeded rotation, noise and JPEG quality.

Deterministic without numpy: the noise is a seeded 64x64 grayscale tile, tiled over the page and blended.
"""

import io
import random

import pypdfium2 as pdfium
from PIL import Image

DPI = 150


def _noise_tile(rng: random.Random) -> Image.Image:
    return Image.frombytes("L", (64, 64), bytes(rng.randrange(256) for _ in range(64 * 64)))


def pdf_to_jpeg(pdf: bytes, seed: int) -> bytes:
    rng = random.Random(f"synthetic-image:{seed}")
    doc = pdfium.PdfDocument(pdf)
    try:
        page = doc[0].render(scale=DPI / 72).to_pil().convert("RGB")
    finally:
        doc.close()
    page = page.rotate(rng.uniform(-1.5, 1.5), resample=Image.Resampling.BICUBIC, expand=False,
                       fillcolor=(255, 255, 255))
    tile = _noise_tile(rng)
    noise = Image.new("L", page.size)
    for x in range(0, page.width, 64):
        for y in range(0, page.height, 64):
            noise.paste(tile, (x, y))
    page = Image.blend(page, Image.merge("RGB", (noise, noise, noise)), 0.06)
    out = io.BytesIO()
    page.save(out, format="JPEG", quality=rng.randint(70, 90), optimize=False)
    return out.getvalue()


def pdf_page_count(pdf: bytes) -> int:
    doc = pdfium.PdfDocument(pdf)
    try:
        return len(doc)
    finally:
        doc.close()
