# README screenshots

Everything here is generated. Don't edit the images by hand, re-run the scripts.

## Web UI (`web/`)

`shoot.mjs` serves the real production build of `ui/` and answers every API
call from `fixtures.mjs` (made-up data: `demo` user, `192.168.1.x`,
`example.com`). It drives headless Chrome over the DevTools protocol
(`cdp.mjs`, no npm packages): sign-in state is faked in `localStorage`, the
widgets get socket events replayed into them, and each scene in `scenes.mjs`
opens windows through the store. Chrome refuses every request to this machine
except the mock server (the Download Station browser gets a fake mirror page),
so nothing real is ever touched. Shots are taken at 1600x1000 @2x, downscaled
to 1x WebP; single-app shots are cropped to their window.

```sh
(cd ui && pnpm install && pnpm build)          # once, or after UI changes
node docs/images/screenshots/shoot.mjs         # all shots
node docs/images/screenshots/shoot.mjs vms     # only shots whose name starts with "vms"
DEBUG=1 node docs/images/screenshots/shoot.mjs # log API calls (unmocked ones say "(default)")
```

Needs `google-chrome` and ImageMagick (`magick`). App icons and store
screenshots load from the public CasaOS AppStore CDN.

## Android app (`app/`)

`phones.sh` frames the Flutter screenshot goldens
(`mobile/test/screenshots/goldens/`, regenerated with
`flutter test --update-goldens test/screenshots`) in a plain phone outline:

```sh
sh docs/images/screenshots/phones.sh
```

Pick other screens by editing the `frame` lines at the bottom of the script.
