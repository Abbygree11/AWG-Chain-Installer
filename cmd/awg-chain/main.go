package main

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Abbygree11/AWG-Chain-Installer/internal/chain"
)

var (
	bg      = color.NRGBA{R: 14, G: 16, B: 30, A: 255}
	surface = color.NRGBA{R: 28, G: 30, B: 49, A: 255}
	sidebar = color.NRGBA{R: 19, G: 21, B: 37, A: 255}
	accent  = color.NRGBA{R: 163, G: 111, B: 255, A: 255}
	muted   = color.NRGBA{R: 164, G: 166, B: 194, A: 255}
	white   = color.NRGBA{R: 241, G: 241, B: 251, A: 255}
	green   = color.NRGBA{R: 107, G: 222, B: 172, A: 255}
)

type purpleTheme struct{}

func (purpleTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary: return accent
	case theme.ColorNameBackground: return bg
	case theme.ColorNameInputBackground: return surface
	case theme.ColorNameButton: return surface
	case theme.ColorNameForeground: return white
	case theme.ColorNamePlaceHolder: return muted
	}
	return theme.DefaultTheme().Color(name, theme.VariantDark)
}
func (purpleTheme) Font(s fyne.TextStyle) fyne.Resource { return theme.DefaultTheme().Font(s) }
func (purpleTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }
func (purpleTheme) Size(n fyne.ThemeSizeName) float32 { return theme.DefaultTheme().Size(n) }

func text(s string, size float32, c color.Color, bold bool) *canvas.Text {
	t := canvas.NewText(s, c)
	t.TextSize = size
	t.TextStyle = fyne.TextStyle{Bold: bold}
	return t
}

func panel(body ...fyne.CanvasObject) fyne.CanvasObject {
	rect := canvas.NewRectangle(surface)
	rect.CornerRadius = 15
	rect.StrokeColor = color.NRGBA{R: 71, G: 66, B: 103, A: 255}
	rect.StrokeWidth = 1
	return container.NewStack(rect, container.NewPadded(container.NewVBox(body...)))
}

func field(label string, entry *widget.Entry) fyne.CanvasObject {
	return container.NewVBox(text(label, 13, muted, false), entry)
}

func serverPanel(title, subtitle string, ip, port, user, password *widget.Entry) fyne.CanvasObject {
	return panel(
		text(title, 20, white, true),
		text(subtitle, 12, muted, false),
		widget.NewSeparator(),
		container.NewGridWithColumns(2,
			field("IPv4-адрес", ip), field("SSH-порт", port)),
		container.NewGridWithColumns(2,
			field("Пользователь", user), field("Пароль", password)),
	)
}

func defaultFolder() string {
	home, err := os.UserHomeDir()
	if err != nil { return "." }
	return filepath.Join(home, "Downloads", "AWG-Chain")
}

func main() {
	a := app.NewWithID("org.awgchain.installer")
	a.Settings().SetTheme(purpleTheme{})
	w := a.NewWindow("AWG Chain Installer")
	w.Resize(fyne.NewSize(1250, 830))
	w.SetFixedSize(false)

	entryIP, exitIP := widget.NewEntry(), widget.NewEntry()
	entryIP.SetPlaceHolder("IPv4 первого VPS")
	exitIP.SetPlaceHolder("IPv4 второго VPS")

	entryPort, exitPort := widget.NewEntry(), widget.NewEntry()
	entryPort.SetText("22"); exitPort.SetText("22")
	entryUser, exitUser := widget.NewEntry(), widget.NewEntry()
	entryUser.SetText("root"); exitUser.SetText("root")
	entryPass, exitPass := widget.NewPasswordEntry(), widget.NewPasswordEntry()
	entryPass.SetPlaceHolder("SSH-пароль")
	exitPass.SetPlaceHolder("SSH-пароль")

	profile := widget.NewEntry()
	profile.SetText("AWG-Finland")
	output := widget.NewLabel(defaultFolder())
	output.Wrapping = fyne.TextWrapBreak

	status := text("Готов к установке", 15, muted, true)
	progress := widget.NewProgressBar()
	progress.SetValue(0)
	detail := widget.NewLabel("Введите данные двух чистых VPS и запустите установку.")
	detail.Wrapping = fyne.TextWrapWord
	logLabel := widget.NewLabel("Здесь появится журнал этапов установки.")
	logLabel.Wrapping = fyne.TextWrapWord
	logScroll := container.NewVScroll(logLabel)
	logScroll.SetMinSize(fyne.NewSize(300, 190))
	result := widget.NewLabel("Файл появится после успешной проверки серверов.")
	result.Wrapping = fyne.TextWrapWord

	browse := widget.NewButton("Выбрать папку", func() {
		dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil { dialog.ShowError(err,w);return }
			if uri != nil { output.SetText(uri.Path()) }
		},w).Show()
	})
	var events []string
	addLog := func(title, detail string) {
		events = append(events, fmt.Sprintf("• %s — %s", title, detail))
		logLabel.SetText(strings.Join(events, "\n"))
		logScroll.ScrollToBottom()
	}

	start := widget.NewButton("▶  Запустить установку", nil)
	start.Importance = widget.HighImportance
	start.OnTapped = func() {
		entryPortNumber, eerr := strconv.Atoi(strings.TrimSpace(entryPort.Text))
		exitPortNumber, xerr := strconv.Atoi(strings.TrimSpace(exitPort.Text))
		if eerr != nil || xerr != nil {
			dialog.ShowError(fmt.Errorf("SSH-порт должен быть числом от 1 до 65535"), w)
			return
		}
		opt := chain.Options{
			Entry: chain.Server{Host:strings.TrimSpace(entryIP.Text),Port:entryPortNumber,User:strings.TrimSpace(entryUser.Text),Password:entryPass.Text},
			Exit: chain.Server{Host:strings.TrimSpace(exitIP.Text),Port:exitPortNumber,User:strings.TrimSpace(exitUser.Text),Password:exitPass.Text},
			Profile:strings.TrimSpace(profile.Text), OutputDir:output.Text,
		}
		if err := opt.Validate(); err != nil { dialog.ShowError(err,w); return }

		dialog.NewConfirm("Начать настройку?",
			"Установщик изменит пакеты, VPN-конфигурации, маршруты и firewall на обоих VPS. Поддерживаются чистые Ubuntu 24.04 LTS с ядром 6.8. SSH-авторизация не меняется. Продолжить?",
			func(ok bool) {
				if !ok { return }
				start.Disable()
				events = nil
				logLabel.SetText("")
				status.Text = "Установка выполняется"
				status.Color = accent
				status.Refresh()
				result.SetText("Идёт настройка двух серверов...")
				go func() {
					installer := chain.Installer{
						AskHostKey:func(address, fingerprint string) bool {
							accepted := make(chan bool,1)
							fyne.Do(func() {
								dialog.NewConfirm("Проверка ключа SSH",
									fmt.Sprintf("Впервые подключаемся к %s\n\nОтпечаток ключа:\n%s\n\nСравни его с отпечатком, показанным в консоли VPS у провайдера. Доверять этому ключу?",address,fingerprint),
									func(ok bool) { accepted <- ok },w).Show()
							})
							return <-accepted
						},
						Progress:func(p float64, title, step string) {
							fyne.Do(func() {
								progress.SetValue(p)
								detail.SetText(step)
								addLog(title,step)
							})
						},
					}
					file, err := installer.Run(context.Background(),opt)
					fyne.Do(func() {
						start.Enable()
						entryPass.SetText("")
						exitPass.SetText("")
						if err != nil {
							status.Text = "Установка остановлена"
							status.Color = color.NRGBA{R:255,G:155,B:159,A:255}
							status.Refresh()
							result.SetText("Файл не создан. Серверы могли быть настроены частично.")
							addLog("Ошибка",err.Error())
							dialog.ShowError(err,w)
							return
						}
						status.Text = "VPN-цепочка настроена"
						status.Color = green
						status.Refresh()
						result.SetText("✓ Создан файл:\n"+file+"\n\nИмпортируй его в Keenetic. Только после импорта можно проверить связь с роутером.")
						addLog("Готово","Файл сохранён: "+file)
						dialog.ShowInformation("Установка завершена","VPN между VPS работает. Конфигурация для Keenetic сохранена:\n"+file,w)
					})
				}()
			},w).Show()
	}

	left := container.NewVBox(
		text("Настройка AWG Chain", 27, white, true),
		text("Keenetic → входной VPS (AWG / IPv4) → выходной VPS (AWG / IPv6) → интернет",13,muted,false),
		widget.NewSeparator(),
		serverPanel("01   Входной сервер", "Получает соединения от домашнего роутера", entryIP,entryPort,entryUser,entryPass),
		serverPanel("02   Выходной сервер", "Отвечает за внешний IP и выход в интернет", exitIP,exitPort,exitUser,exitPass),
		panel(text("Название профиля",15,white,true),profile,
			text("Куда сохранить keenetic.conf",15,white,true),
			container.NewBorder(nil,nil,nil,browse,output)),
		start,
		text("Пароли используются только для SSH и не сохраняются. SSH-ключ сервера проверяется при первом подключении.",12,muted,false),
	)
	leftScroll := container.NewVScroll(container.NewPadded(left))

	route := panel(
		text("Маршрут подключения",19,white,true),
		container.NewHBox(
			text("▣ Keenetic",14,white,true),
			text("➜",18,green,true),
			text("▤ Входной VPS",14,white,true),
			text("➜",18,green,true),
			text("▤ Выходной VPS",14,white,true),
		),
		text("AWG 3.1 поверх IPv4, затем AWG 3.1 поверх IPv6",12,muted,false),
	)
	steps := panel(
		text("Что сделает программа",19,white,true),
		text("01  Проверит SSH, Ubuntu 24.04, IPv6 и порты",14,white,false),
		text("02  Установит подписанные пакеты AWG 3.1",14,white,false),
		text("03  Создаст ключи и два VPN-туннеля",14,white,false),
		text("04  Настроит PBR, NAT и kill switch",14,white,false),
		text("05  Проверит выходной IP и автозапуск",14,white,false),
		text("06  Сохранит готовый файл для Keenetic",14,white,false),
	)
	running := panel(text("Ход установки",19,white,true),status,progress,detail,logScroll)
	finished := panel(text("Результат",19,green,true),result)
	right := container.NewVScroll(container.NewPadded(container.NewVBox(route,steps,running,finished)))

	side := canvas.NewRectangle(sidebar)
	side.CornerRadius = 12
	navigation := container.NewStack(side,container.NewPadded(container.NewVBox(
		text("◈",47,accent,true),
		text("AWG Chain",22,white,true),
		text("Installer",22,white,true),
		text("Двухузловой VPN",13,muted,false),
		widget.NewSeparator(),
		text("●  Установка",15,accent,true),
		text("   Ubuntu 24.04 LTS",12,muted,false),
		text("   AmneziaWG 3.1",12,muted,false),
		text("   KeeneticOS 5.2+",12,muted,false),
		widget.NewSeparator(),
		text("v0.1.0",12,muted,false),
	)))
	split := container.NewHSplit(leftScroll,right)
	split.Offset = 0.55
	mainArea := container.NewBorder(nil,nil,container.NewGridWrap(fyne.NewSize(212,710),navigation),nil,split)
	background := canvas.NewRectangle(bg)
	w.SetContent(container.NewStack(background,mainArea))
	w.ShowAndRun()
}
