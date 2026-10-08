// ytamp — a beautiful terminal YouTube music player.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sarvarbek-566/ytamp/internal/auth"
	"github.com/sarvarbek-566/ytamp/internal/config"
	"github.com/sarvarbek-566/ytamp/internal/store"
	"github.com/sarvarbek-566/ytamp/internal/theme"
	"github.com/sarvarbek-566/ytamp/internal/ui"
	"github.com/sarvarbek-566/ytamp/internal/ytdl"
)

var version = "0.1.0"

func main() {
	var (
		login   = flag.Bool("login", false, "YouTube akkauntga kirish (brauzer yoki Google OAuth)")
		logout  = flag.Bool("logout", false, "akkauntdan chiqish va keshni tozalash")
		guest   = flag.Bool("guest", false, "akkauntsiz ishga tushirish")
		setup   = flag.Bool("setup", false, "OAuth sozlash bo'yicha qo'llanma va config'ni ochish")
		th      = flag.String("theme", "", "mavzu: "+strings.Join(theme.Names, ", "))
		showVer = flag.Bool("version", false, "versiyani ko'rsatish")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "ytamp %s — terminal uchun YouTube musiqa pleyeri\n\n", version)
		fmt.Fprintf(os.Stderr, "Foydalanish:\n  ytamp [flaglar] [qidiruv so'zi | YouTube havola]\n\nFlaglar:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nMisollar:\n  ytamp\n  ytamp the weeknd starboy\n  ytamp https://youtu.be/34Na4j8AVgA\n")
	}
	flag.Parse()

	if *showVer {
		fmt.Println("ytamp", version)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		fail("config o'qilmadi (%s): %v", config.Path(), err)
	}
	if *th != "" {
		cfg.Theme = *th
	}
	ytdl.CookiesFromBrowser = cfg.CookiesFromBrowser

	switch {
	case *logout:
		if err := auth.Logout(); err != nil {
			fail("%v", err)
		}
		if cfg.CookiesFromBrowser != "" {
			if err := config.Set("cookies_from_browser", ""); err != nil {
				fail("%v", err)
			}
		}
		store.ClearLibrary()
		fmt.Println("✓ Akkauntdan chiqildi")
		return
	case *setup:
		runSetup()
		return
	}

	for _, dep := range []string{"mpv", "yt-dlp"} {
		if _, err := exec.LookPath(dep); err != nil {
			fail("%s topilmadi. O'rnating: sudo pacman -S %s", dep, dep)
		}
	}

	m := ui.New(ui.Options{
		Config:     cfg,
		ForceLogin: *login,
		Guest:      *guest,
		PlayQuery:  strings.Join(flag.Args(), " "),
	})
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())

	// SIGTERM/SIGHUP (e.g. terminal closed) must still stop mpv cleanly.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGHUP)
	go func() { <-sig; p.Quit() }()

	defer m.Shutdown()
	if _, err := p.Run(); err != nil {
		m.Shutdown()
		fail("%v", err)
	}
}

func runSetup() {
	fmt.Print(`
  Eng oson yo'l: ytamp --login → brauzeringizni tanlang (Google Cloud kerak emas).
  Quyidagi qadamlar faqat Google OAuth usuli uchun:

  ytamp · YouTube akkauntini ulash
  ────────────────────────────────
  1. https://console.cloud.google.com/projectcreate — yangi loyiha yarating
  2. APIs & Services → Library → "YouTube Data API v3" → Enable
  3. OAuth consent screen → External → o'zingizni "Test users"ga qo'shing
  4. Credentials → Create credentials → OAuth client ID
       Application type: "TVs and Limited Input devices"
  5. Client ID va Client secret'ni config'ga yozing:
       ` + config.Path() + `
  6. ytamp --login

  Eslatma: consent screen "Testing" holatida bo'lsa, Google tokenni 7 kunda
  bekor qiladi. Doimiy ishlashi uchun uni "In production"ga o'tkazing
  (shaxsiy foydalanishda Google tekshiruvi shart emas).

`)
	if _, err := config.Load(); err != nil {
		fail("%v", err)
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "nvim"
	}
	if _, err := exec.LookPath(editor); err != nil {
		return
	}
	fmt.Print("  Config hozir ochilsinmi? [Y/n] ")
	var a string
	_, _ = fmt.Scanln(&a)
	if a == "" || strings.EqualFold(a, "y") {
		c := exec.Command(editor, config.Path())
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		_ = c.Run()
	}
}

func fail(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "ytamp: "+f+"\n", a...)
	os.Exit(1)
}
