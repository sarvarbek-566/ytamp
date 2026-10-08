# ytamp

Terminal uchun YouTube musiqa pleyeri. YouTube akkauntingizdagi yoqtirilgan
qo'shiqlar va playlistlarni eshitish, qidirish, cheksiz radio, sinxron
qo'shiq matni va haqiqiy audio spektr — hammasi bitta chiroyli TUI'da.

```
 ▶ ytamp                1 Qo'shiqlar  ·  2 Navbat  ·  3 Matn                ● siz
╭─ Kutubxona ────────╮ ╭─ Yoqtirilganlar ─────────────────────────────── 2/248 ─╮
│ ♥ Yoqtirilganlar 248│ │    #  Nomi                 Ijrochi            Vaqt  │
│ ◷ Tarix          31 │ │  ▃▇▂  Starboy              The Weeknd         3:50  │
│ ⌕ Qidiruv           │ │    3  Shape of You         Ed Sheeran         3:53  │
╰────────────────────╯ ╰──────────────────────────────────────────────────────╯
╭─ ▶ Ijroda ─────────────────────────────────────────────────── opus · 128k ─╮
│ Starboy  —  The Weeknd                                              2/248 ♥ │
│ █ █ ▇ █ ▆ █ █ ▅ ▇ █ ▆ ▄ ▅ ▆ ▃ ▄ ▂ ▃ ▁ ▂ ▁                                   │
│ 1:42 ━━━━━━━━━━━━━━━━━━━━━●────────────────────────────────────────── 3:50 │
│   ⇄    ◁◁   ❚❚   ▷▷    ↻    ✦ radio                       🔉 ▮▮▮▮▮▮▮▯▯▯ 70% │
╰────────────────────────────────────────────────────────────────────────────╯
```

## Imkoniyatlar

- **YouTube akkaunt** — brauzer sessiyasi orqali (sozlashsiz) yoki Google OAuth; yoqtirilganlar, playlistlar, ♥ qo'yish, playlistga qo'shish
- **Kvotasiz qidiruv** — qidiruv va radio yt-dlp orqali, API kvotasi faqat kutubxona uchun sarflanadi
- **Cheksiz radio** — navbat tugasa, YouTube Mix asosida o'xshash qo'shiqlar avtomatik qo'shiladi
- **Sinxron qo'shiq matni** — [lrclib.net](https://lrclib.net) orqali, karaoke uslubida
- **Haqiqiy spektr vizualizatori** — FFT; faqat ytamp'ning o'z PipeWire oqimidan, boshqa ilovalar aralashmaydi
- **MPRIS** — klaviaturadagi media tugmalari, `playerctl`, Waybar/Omarchy bar
- **Omarchy mavzusi** — joriy Omarchy temasidan ranglar avtomatik olinadi (+5 ta tayyor mavzu)
- **Sessiyani tiklash** — navbat, pozitsiya va ovoz keyingi ochilishda tiklanadi
- **Uyqu taymeri** — 15/30/60/90 daqiqa, oxirgi 30 soniyada ovoz sekin pasayadi
- Navbatni boshqarish, aralash/takrorlash, filtr, desktop bildirishnomalar, sichqoncha g'ildiragi
- **Ishonchlilik:** YouTube IP'ni cheklasa ham ishlaydi (`web_embedded` client fallback), oqim xato bersa
  boshqa client'lar bilan qayta urinadi, ytamp qanday yopilmasin (hatto `kill -9`) mpv yetim qolmaydi

## O'rnatish

Talablar: `mpv`, `yt-dlp`, Go 1.26+. Ixtiyoriy: `pw-record` (vizualizator), `wl-copy`, `notify-send`.

```sh
sudo pacman -S mpv yt-dlp     # Arch / Omarchy
go install github.com/sarvarbek-566/ytamp/cmd/ytamp@latest
```

yoki manbadan:

```sh
git clone https://github.com/sarvarbek-566/ytamp && cd ytamp
make install                  # → ~/.local/bin/ytamp
```

## Ishlatish

```sh
ytamp                            # ochish
ytamp the weeknd starboy         # qidirib darhol ijro etish
ytamp https://youtu.be/...       # havolani ijro etish
ytamp --guest                    # akkauntsiz
ytamp --theme catppuccin
```

### YouTube akkauntga kirish

```sh
ytamp --login        # yoki dastur ichida: A
```

**Brauzer orqali (tavsiya etiladi)** — Firefox, Chrome, Chromium, Brave, Edge… ro'yxatdan
brauzeringizni tanlang. ytamp shu brauzerdagi YouTube sessiyangizdan foydalanadi: Google Cloud
sozlash shart emas. Yoqtirilganlar, playlistlar, "Keyinroq ko'rish", ♥ va playlistga qo'shish ishlaydi.
Cookie'lar faqat kompyuteringizda o'qiladi va faqat YouTube'ga yuboriladi. Tanlov
`cookies_from_browser` sifatida config'da saqlanadi va keyingi safar avtomatik kiriladi.

**Google OAuth** — o'z Google Cloud client'ingiz bilan (rasmiy API):

1. [Google Cloud Console](https://console.cloud.google.com/projectcreate)'da loyiha yarating, **YouTube Data API v3**'ni yoqing
2. OAuth consent screen → External → o'zingizni *Test users*'ga qo'shing
3. Credentials → OAuth client ID → **TVs and Limited Input devices**
4. `ytamp --login` → *Google OAuth* → Client ID va secret'ni kiriting (Google qabul qilgach saqlanadi)
5. Ko'rsatilgan kodni telefoningizda `google.com/device`'ga kiriting

> Consent screen "Testing" holatida bo'lsa, Google tokenni 7 kunda bekor qiladi — unda qayta kiring
> yoki "In production"ga o'tkazing.

Chiqish: `A` → *Akkauntdan chiqish*, yoki `ytamp --logout`.

### YouTube "bot emasligingizni tasdiqlang" desa

ytamp avval cookie'siz client'larni sinaydi — odatda shu yetarli. Agar baribir
`YouTube bot tekshiruvi` xabari chiqsa, brauzeringizdagi YouTube sessiyasidan
foydalanishga ruxsat bering (`~/.config/ytamp/config.toml`):

```toml
cookies_from_browser = "firefox"   # yoki chromium, brave, …
```

## Tugmalar

| Tugma | Amal | Tugma | Amal |
|---|---|---|---|
| `space` | ijro / pauza | `/` | qidiruv yoki havola |
| `n` `p` | keyingi / oldingi | `f` | ro'yxatni filtrlash |
| `←` `→` | ±5 s | `H` `L` | ±30 s |
| `+` `-` `m` | ovoz / ovozsiz | `1` `2` `3` | qo'shiqlar / navbat / matn |
| `s` `r` | aralash / takrorlash | `tab` `h` `l` | panel |
| `a` `e` | navbatga / keyingi bo'lib | `x` `J` `K` | navbatdan o'chirish / surish |
| `w` | qo'shiqdan radio | `F` `*` | yoqtirish ♥ |
| `b` | playlistga qo'shish | `R` | kutubxonani yangilash |
| `z` | uyqu taymeri | `t` `v` | mavzu / vizualizator |
| `o` `c` | brauzerda ochish / havolani nusxalash | `?` | yordam |
| `A` | akkauntga kirish / chiqish | `q` | chiqish |

## Fayllar

| Yo'l | Mazmuni |
|---|---|
| `~/.config/ytamp/config.toml` | sozlamalar |
| `~/.local/state/ytamp/token.json` | OAuth token (0600) |
| `~/.local/state/ytamp/library.json` | kutubxona keshi (kvotani tejaydi) |
| `~/.local/state/ytamp/session.json`, `history.json` | sessiya va tarix |
| `~/.local/state/ytamp/ytamp.log` | xatolar logi |

## Testlar

```sh
make test                          # unit + ssenariy + fuzz (internetsiz)
YTAMP_NET=1 go test ./...          # + haqiqiy YouTube, lrclib va mpv oqimi
YTAMP_BROWSER=firefox go test ./internal/ytdl -run Browser   # brauzer sessiyasi bilan kutubxona
YTAMP_FUZZ_SEEDS=200 go test ./internal/ui -run Fuzz   # chuqur fuzz
```

- **Ssenariy testlari** (`internal/ui/scenario_test.go`) — soxta mpv va YouTube bilan 40+ real foydalanish holati
- **Fuzz** — tasodifiy tugmalar, mpv hodisalari, tarmoq xatolari va oyna o'lchamlari; har qadamda
  invariantlar va kadr aynan `w×h` ekanligi tekshiriladi
- **mpv integratsiyasi** — haqiqiy mpv (`--ao=null`), jumladan `kill -9` holatida yetim jarayon qolmasligi

## Arxitektura

```
cmd/ytamp            CLI, flaglar, signal'lar
internal/ui          Bubble Tea modeli: holat, tugmalar, render
internal/player      mpv'ni JSON IPC orqali boshqarish (ytdl hook → yt-dlp)
internal/youtube     YouTube Data API v3 (kutubxona, rate, playlist)
internal/ytdl        yt-dlp: qidiruv, radio (Mix), qayta urinish uchun oqim havolasi
internal/auth        OAuth device flow, avtomatik yangilanuvchi token
internal/viz         pw-record → FFT → log-chastota ustunlari
internal/lyrics      lrclib.net sinxron matn
internal/mpris       D-Bus MPRIS2 server
internal/store       kesh, tarix, sessiya
internal/theme       mavzular, Omarchy integratsiyasi
```

> Eslatma: audio oqim yt-dlp orqali olinadi — bu YouTube ToS'ning kulrang hududi;
> shaxsiy foydalanish uchun mo'ljallangan.

## Litsenziya

[MIT](LICENSE)
