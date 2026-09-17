# Launcher icons

Both Wails projects (`darkspin` and `darkspinner`) use the same artwork:

- `build/windows/icon.ico` contains 16, 20, 24, 32, 40, 48, 64, 128, and 256 pixel frames. The 16 pixel frame preserves the supplied `image.webp` artwork exactly; larger frames are resized from the supplied `logo.webp` with transparency preserved.
- `build/appicon.png` is the larger logo at 512×512. Wails uses it to generate the macOS application icon; Darkspinner also embeds it for the Linux window icon.
- `frontend/public/favicon.ico`, `icon-16.png`, and `icon-32.png` provide the frontend/browser icons, referenced in each frontend's `index.html`.

Keep the custom Windows ICO when updating the artwork. Wails preserves an existing `build/windows/icon.ico`; deleting it lets Wails regenerate every size from `appicon.png`, which loses the dedicated 16 pixel artwork. Keep both projects' matching assets synchronized.
