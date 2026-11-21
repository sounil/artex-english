# Screenshots

This folder holds two separate sets of captures. To keep them from looking mixed, here
is what each one is.

- **`en/` — real UI of ARTEX in the default English display language.** This is what the fork delivers. The
  main docs [`../README.md`](../README.md) and [`../README.ko.md`](../README.ko.md) use
  these images. For the delivered screens, open [`en/`](en/).
- **The `*.png` files in the root — upstream (Chinese edition) UI captures.** They are
  **preserved as-is** so the fork can be diffed against the upstream repository 1:1, and
  the preserved Chinese README [`../README.zh.md`](../README.zh.md) references them. The
  Korean and English docs do not use them.
- **`wx.png` — the upstream author's WeChat channel promo image.** Kept so that
  [`../README.zh.md`](../README.zh.md) still renders unchanged. It is unrelated to this
  fork.

In short, the Chinese screens you see first in the root are there for upstream
preservation; the **delivered English screens live in [`en/`](en/).** See the root README
for why this fork keeps the originals alongside (localization transparency and upstream
comparison).
