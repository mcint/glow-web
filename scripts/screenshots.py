"""Re-capture the README screenshots against a running glow-web.

The Makefile starts a glow-web server on localhost, then invokes this script.
We use playwright with the system chromium (no playwright-managed browser
download), prime localStorage so the sidebar shot has the panel open, and
write five PNGs into $SHOT_DIR.
"""

import asyncio
import os
import sys
from playwright.async_api import async_playwright

PORT = os.environ.get("PORT", "18099")
SHOT_DIR = os.environ.get("SHOT_DIR", "docs/screenshots")
CHROMIUM = os.environ.get("CHROMIUM", "/opt/homebrew/bin/chromium")
BASE = f"http://127.0.0.1:{PORT}"


async def shot(page, url, path):
    await page.goto(url)
    await page.wait_for_load_state("networkidle")
    await page.screenshot(path=path)
    print(f"wrote {path}", file=sys.stderr)


async def main():
    os.makedirs(SHOT_DIR, exist_ok=True)
    async with async_playwright() as p:
        browser = await p.chromium.launch(executable_path=CHROMIUM)
        ctx = await browser.new_context(viewport={"width": 1280, "height": 900})
        page = await ctx.new_page()

        await shot(page, f"{BASE}/", f"{SHOT_DIR}/index.png")
        await shot(page, f"{BASE}/specs/spec.md", f"{SHOT_DIR}/view.png")
        await shot(page, f"{BASE}/specs/spec.md?view=markup", f"{SHOT_DIR}/markup.png")

        # KEY_PREFIX is a script-global on every page (per-project hash); read
        # it back so we can prime localStorage with the right namespaced keys.
        prefix = await page.evaluate("KEY_PREFIX")

        await page.evaluate(f"localStorage.setItem('{prefix}palette-open', '1')")
        await shot(page, f"{BASE}/specs/spec.md", f"{SHOT_DIR}/sidebar.png")

        await page.evaluate(f"localStorage.setItem('{prefix}theme', 'dark')")
        await page.evaluate(f"localStorage.removeItem('{prefix}palette-open')")
        await shot(page, f"{BASE}/specs/spec.md", f"{SHOT_DIR}/dark.png")

        await browser.close()


asyncio.run(main())
