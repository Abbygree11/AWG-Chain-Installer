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
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Abbygree11/AWG-Chain-Installer/internal/chain"
)

func defaultFolder() string {
	home, err := os.UserHomeDir()
	if err != nil { return "." }
	return filepath.Join(home, "Downloads", "AWG-Chain")
}

func main() {
	a := app.NewWithID("org.awgchain.installer")
	a.Settings().SetTheme(chainTheme{})

	w := a.NewWindow("AWG Chain Installer")
	w.Resize(fyne.NewSize(1510, 980))

	entryIP, exitIP := widget.NewEntry(), widget.NewEntry()
	entryIP.SetPlaceHolder("IPv4 первого VPS")
	exitIP.SetPlaceHolder("IPv4 второго VPS")

	entryPort, exitPort := widget.NewEntry(), widget.NewEntry()
	entryPort.SetText("22")
	exitPort.SetText("22")
	entryUser, exitUser := widget.NewEntry(), widget.NewEntry()
	entryUser.SetText("root")
	exitUser.SetText("root")
	entryPass, exitPass := widget.NewPasswordEntry(), widget.NewPasswordEntry()
	entryPass.SetPlaceHolder("SSH-пароль")
	exitPass.SetPlaceHolder("SSH-пароль")
	profile := widget.NewEntry()
	profile.SetText("AWG-Finland")

	output := widget.NewLabel(defaultFolder())
	output.Wrapping = fyne.TextWrapBreak
	settingsOutput := widget.NewLabel(defaultFolder())
	settingsOutput.Wrapping = fyne.TextWrapBreak
	browse := widget.NewButtonWithIcon("Выбрать папку", theme.FolderIcon(), func() {
		dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil { dialog.ShowError(err, w); return }
			if uri != nil { output.SetText(uri.Path()); settingsOutput.SetText(uri.Path()) }
		}, w).Show()
	})

	status := txt("Готов к установке", 15, secondary, true)
	logStatus := txt("Готов к установке", 15, secondary, true)
	progress := widget.NewProgressBar()
	progress.SetValue(0)
	logProgress := widget.NewProgressBar()
	logProgress.SetValue(0)
	detail := widget.NewLabel("Введите данные VPS и запустите установку.")
	detail.Wrapping = fyne.TextWrapWord

	logLabel := widget.NewLabel("После запуска здесь будут отображаться действия установщика.")
	logLabel.Wrapping = fyne.TextWrapWord
	lastLog := widget.NewLabel("Подробные сообщения можно открыть на вкладке «Логи».")
	lastLog.Wrapping = fyne.TextWrapWord
	logScroll := container.NewVScroll(logLabel)
	logScroll.SetMinSize(fyne.NewSize(450, 460))

	resultNote := widget.NewLabel("После успешной установки здесь появится путь к файлу для Keenetic.")
	resultNote.Wrapping = fyne.TextWrapWord
	resultNote.TextStyle = fyne.TextStyle{}
	resultState := txt("После установки", 14, secondary, true)
	var events []string
	addLog := func(title, description string) {
		events = append(events, title+" — "+description)
		logLabel.SetText(strings.Join(events, "\n\n"))
		lastLog.SetText(events[len(events)-1])
		logScroll.ScrollToBottom()
	}

	mainHeader := container.NewHBox(
		imageIcon("chain-header", linkSVG, 76),
		container.NewVBox(
			txt("Настройка AWG Chain", 31, snow, true),
			txt("Автоматическая установка двухузлового VPN", 15, secondary, false),
			txt("Входной сервер → выходной сервер • AmneziaWG 3.1", 14, secondary, false),
		),
	)

	fileCard := standardCard(
		txt("Название подключения", 16, snow, true),
		labelledEntry("Имя профиля для файла Keenetic", profile),
		txt("Куда сохранить готовый файл", 16, snow, true),
		container.NewBorder(nil, nil, nil, browse, output),
	)

	var start *mainAction
	start = newMainAction(func() {
		entryPortNumber, eerr := strconv.Atoi(strings.TrimSpace(entryPort.Text))
		exitPortNumber, xerr := strconv.Atoi(strings.TrimSpace(exitPort.Text))
		if eerr != nil || xerr != nil {
			dialog.ShowError(fmt.Errorf("SSH-порт должен быть целым числом от 1 до 65535"), w)
			return
		}
		opt := chain.Options{
			Entry: chain.Server{
				Host: strings.TrimSpace(entryIP.Text),
				Port: entryPortNumber,
				User: strings.TrimSpace(entryUser.Text),
				Password: entryPass.Text,
			},
			Exit: chain.Server{
				Host: strings.TrimSpace(exitIP.Text),
				Port: exitPortNumber,
				User: strings.TrimSpace(exitUser.Text),
				Password: exitPass.Text,
			},
			Profile: strings.TrimSpace(profile.Text),
			OutputDir: output.Text,
		}
		if err := opt.Validate(); err != nil { dialog.ShowError(err, w); return }

		dialog.NewConfirm("Начать настройку VPN?",
			"Установщик изменит VPN, маршруты и firewall на обоих VPS. Нужны чистые Ubuntu 24.04 LTS с ядром 6.8. SSH-настройки не меняются. Продолжить?",
			func(ok bool) {
				if !ok { return }
				start.SetBusy(true)
				events = nil
				logLabel.SetText("")
				lastLog.SetText("Начинаем подключение…")
				progress.SetValue(0)
				logProgress.SetValue(0)
				logStatus.Text = "Установка выполняется"
				logStatus.Color = purple
				logStatus.Refresh()
				status.Text = "Установка выполняется"
				status.Color = purple
				status.Refresh()
				resultState.Text = "Установка выполняется"
				resultState.Color = purple
				resultState.Refresh()
				resultNote.SetText("Идёт настройка VPS. Конфигурация будет сохранена после успешных проверок.")
				go func() {
					installer := chain.Installer{
						AskHostKey: func(address, fingerprint string) bool {
							accepted := make(chan bool, 1)
							fyne.Do(func() {
								dialog.NewConfirm("Проверка SSH-ключа",
									fmt.Sprintf("Впервые подключаемся к %s\n\nSHA256-отпечаток ключа:\n%s\n\nСверь его с отпечатком из консоли VPS у провайдера. Доверять этому ключу?", address, fingerprint),
									func(ok bool) { accepted <- ok }, w).Show()
							})
							return <-accepted
						},
						Progress: func(p float64, title, step string) {
							fyne.Do(func() {
								progress.SetValue(p)
								logProgress.SetValue(p)
								detail.SetText(step)
								addLog(title, step)
							})
						},
					}
					file, err := installer.Run(context.Background(), opt)
					fyne.Do(func() {
						start.SetBusy(false)
						entryPass.SetText("")
						exitPass.SetText("")
						if err != nil {
							status.Text = "Установка остановлена"
							logStatus.Text = status.Text
							logStatus.Color = color.NRGBA{R: 255, G: 151, B: 155, A: 255}
							logStatus.Refresh()
							status.Color = color.NRGBA{R: 255, G: 151, B: 155, A: 255}
							status.Refresh()
							resultState.Text = "Требуется проверка"
							resultState.Color = status.Color
							resultState.Refresh()
							resultNote.SetText("Файл не создан. На VPS могли остаться частичные изменения.")
							addLog("Ошибка", err.Error())
							dialog.ShowError(err, w)
							return
						}
						status.Text = "VPN-цепочка настроена"
						logStatus.Text = status.Text
						logStatus.Color = mint
						logStatus.Refresh()
						status.Color = mint
						status.Refresh()
						resultState.Text = "Настройка завершена"
						resultState.Color = mint
						resultState.Refresh()
						resultNote.SetText("Готовый конфигурационный файл:\n"+file+"\n\nИмпортируй его в Keenetic и выбери нужную политику маршрутизации.")
						addLog("Готово", "Файл сохранён: "+file)
						dialog.ShowInformation("Установка завершена", "Туннель между VPS проверен. Файл для Keenetic:\n"+file, w)
					})
				}()
			}, w).Show()
	})

	setupLeft := container.NewVScroll(pad(23,0,25,23,container.NewVBox(
		mainHeader,
		serverCard("Входной сервер", "Принимает подключение от Keenetic", entryIP, entryPort, entryUser, entryPass),
		serverCard("Выходной сервер", "Выпускает трафик в интернет", exitIP, exitPort, exitUser, exitPass),
		fileCard,
		start,
		txt("Пароли используются только для SSH и не сохраняются программой.", 12, secondary, false),
	)))

	routeNode := func(iconSVG, iconName, label string) fyne.CanvasObject {
		l := widget.NewLabel(label)
		l.Alignment = fyne.TextAlignCenter
		l.Wrapping = fyne.TextWrapWord
		return container.NewVBox(
			container.NewCenter(imageIcon(iconName, iconSVG, 55)),
			l,
		)
	}
	routeCard := roundedPanel(
		color.NRGBA{R: 35, G: 28, B: 65, A: 255},
		color.NRGBA{R: 112, G: 78, B: 166, A: 255},
		20,
		container.NewVBox(
			container.NewGridWithColumns(5,
				routeNode(routerSVG, "router-path", "Keenetic"),
				container.NewCenter(imageIcon("arrow-one", arrowSVG, 30)),
				routeNode(serverSVG, "entry-path", "Входной\nсервер"),
				container.NewCenter(imageIcon("arrow-two", arrowSVG, 30)),
				routeNode(serverSVG, "exit-path", "Выходной\nсервер"),
			),
			container.NewCenter(txt("AWG / IPv4                                  AWG / IPv6", 12, secondary, false)),
		),
	)

	steps := standardCard(
		container.NewHBox(imageIcon("settings-title", serverSVG, 28), txt("Что сделает программа", 21, snow, true)),
		widget.NewSeparator(),
		stepRow("1", "Подключится к обоим VPS по SSH"),
		stepRow("2", "Проверит систему, IPv6 и порты"),
		stepRow("3", "Установит AmneziaWG 3.1"),
		stepRow("4", "Настроит два VPN-туннеля"),
		stepRow("5", "Создаст PBR, NAT и kill switch"),
		stepRow("6", "Применит ограничения firewall"),
		stepRow("7", "Проверит соединение и автозапуск"),
		stepRow("8", "Сохранит файл для Keenetic"),
	)

	resultCard := statusCard(
		container.NewHBox(imageIcon("shield", shieldSVG, 34), txt("Результат", 21, mint, true)),
		resultState,
		resultNote,
	)

	runningPanel := standardCard(
		txt("Ход установки", 19, snow, true),
		status,
		progress,
		detail,
		lastLog,
	)

	setupRight := container.NewVScroll(pad(23,22,25,0,container.NewVBox(
		routeCard,
		steps,
		resultCard,
		runningPanel,
	)))

	mainInstall := container.New(splitCols{ratio:0.56,gap:15},setupLeft,setupRight)
	logsPage := container.NewVScroll(pad(32,40,32,40,
		standardCard(
			txt("Журнал установки", 28, snow, true),
			txt("События установки без сохранения SSH-паролей и приватных ключей", 14, secondary, false),
			widget.NewSeparator(),
			logStatus,
			logProgress,
			logScroll,
			widget.NewButtonWithIcon("Скопировать журнал", theme.ContentCopyIcon(), func() {
				w.Clipboard().SetContent(strings.Join(events,"\n"))
				dialog.ShowInformation("Скопировано", "Журнал установки скопирован в буфер обмена.", w)
			}),
		),
	))

	settingsPage := container.NewVScroll(pad(32,40,32,40,
		standardCard(
			txt("Настройки", 28, snow, true),
			txt("Первый релиз оптимизирован под чистые Ubuntu 24.04 LTS с ядром Linux 6.8", 15, secondary, false),
			widget.NewSeparator(),
			txt("Папка результата", 18, snow, true),
			container.NewBorder(nil,nil,nil,widget.NewButtonWithIcon("Выбрать папку",theme.FolderIcon(),func(){browse.OnTapped()}),settingsOutput),
			txt("Порты AWG: UDP 51821 (вход), UDP 51820 (выход)", 14, secondary, false),
			txt("Подсети: 10.86.66.0/24 и 10.77.77.0/30", 14, secondary, false),
			txt("Установщик не изменяет SSH-аутентификацию и не перезаписывает уже существующие VPN-профили.", 14, secondary, false),
		),
	))

	aboutPage := container.NewVScroll(pad(32,40,32,40,
		standardCard(
			container.NewHBox(imageIcon("about-logo",logoSVG,72),
				container.NewVBox(txt("AWG Chain Installer",28,snow,true),txt("Версия 0.1.0 • Go + Fyne",15,secondary,false))),
			widget.NewSeparator(),
			txt("Автоматическая настройка двухузлового AmneziaWG 3.1",19,snow,true),
			txt("Keenetic → входной VPS по IPv4 → выходной VPS по IPv6 → интернет",14,secondary,false),
			txt("Встроенные проверки SSH-ключей, PBR, NAT, kill switch и сохранение файла .conf для Keenetic.",14,secondary,false),
			txt("Проект не связан с разработчиками Amnezia или Keenetic.",13,secondary,false),
		),
	))

	managedPage := makeManagedPage(w)
	contents := container.NewStack(mainInstall)
	var navButtons []*navAction
	setPage := func(page fyne.CanvasObject, current int) {
		contents.Objects = []fyne.CanvasObject{page}
		contents.Refresh()
		for i, btn := range navButtons {
			btn.SetSelected(i==current)
		}
	}

	installButton := newNavAction("Установка",theme.SettingsIcon(),func(){setPage(mainInstall,0)})
	managedButton := newNavAction("Amnezia VPN",theme.SearchIcon(),func(){setPage(managedPage,1)})
	logsButton := newNavAction("Логи",theme.DocumentIcon(),func(){setPage(logsPage,2)})
	settingsButton := newNavAction("Настройки",theme.SettingsIcon(),func(){setPage(settingsPage,3)})
	aboutButton := newNavAction("О программе",theme.InfoIcon(),func(){setPage(aboutPage,4)})
	navButtons = []*navAction{installButton,managedButton,logsButton,settingsButton,aboutButton}
	installButton.SetSelected(true)

	navTop := container.NewVBox(
		container.NewCenter(imageIcon("brand-logo",logoSVG,80)),
		container.NewCenter(txt("AWG Chain",24,snow,true)),
		container.NewCenter(txt("Installer",24,snow,true)),
		container.NewCenter(txt("Двухузловой VPN",14,secondary,false)),
		container.NewCenter(txt("на AmneziaWG 3.1",13,secondary,false)),
		widget.NewSeparator(),
		installButton,
		managedButton,
		logsButton,
		settingsButton,
		aboutButton,
	)
	bottom := container.NewVBox(txt("v0.1.0",13,secondary,false))
	navContent := pad(25,12,20,12,container.NewBorder(nil,bottom,nil,nil,navTop))
	sidebar := container.NewStack(sidebarBackground(),navContent)
	sidebarFixed := container.New(fixedColumn{width:250},sidebar)

	w.SetContent(container.NewBorder(nil,nil,sidebarFixed,nil,contents))
	w.ShowAndRun()
}
