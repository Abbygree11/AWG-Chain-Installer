package main

import (
	"context"
	"fmt"
	"image/color"
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

	resultNote := widget.NewLabel("После установки сервер появится в Amnezia VPN через поиск установленных сервисов.")
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
			txt("AWG Chain · Amnezia-managed", 31, snow, true),
			txt("Двухузловой VPN с управлением пользователями в Amnezia VPN", 15, secondary, false),
			txt("Docker AWG 3.1 → awg-mediator / IPv6 → awg-destination", 14, secondary, false),
		),
	)

	// Curated alternative allocations avoid changing just one part of the
	// multi-network chain and leaving NAT / Docker / AWG out of sync.
	presets := chain.NetworkPresets()
	const customName = "Свой набор подсетей…"
	names := make([]string, 0, len(presets)+1)
	byName := make(map[string]chain.NetworkPlan)
	for _, preset := range presets {
		names = append(names, preset.Name)
		byName[preset.Name] = preset.Plan
	}
	names = append(names, customName)
	selectNetworks := widget.NewSelect(names, nil)
	networkDescription := widget.NewLabel("")
	networkDescription.Wrapping = fyne.TextWrapWord
	customClients := widget.NewEntry()
	customHop := widget.NewEntry()
	customDocker := widget.NewEntry()
	customDNS := widget.NewEntry()
	defaultNetworks := chain.DefaultNetworkPlan()
	customClients.SetText(defaultNetworks.Clients)
	customHop.SetText(defaultNetworks.Hop)
	customDocker.SetText(defaultNetworks.Docker)
	customDNS.SetText(defaultNetworks.DNS)
	customNetworks := container.NewVBox(
		labelledEntry("Клиентская AWG · /24", customClients),
		labelledEntry("Межсерверная сеть · /30", customHop),
		labelledEntry("Docker — выход через второй VPS · /24", customDocker),
		labelledEntry("Docker — служебная сеть Amnezia · /24", customDNS),
	)
	customNetworks.Hide()
	selectedNetworks := func() chain.NetworkPlan {
		if selectNetworks.Selected == customName {
			return chain.NetworkPlan{
				Clients: strings.TrimSpace(customClients.Text),
				Hop: strings.TrimSpace(customHop.Text),
				Docker: strings.TrimSpace(customDocker.Text),
				DNS: strings.TrimSpace(customDNS.Text),
			}
		}
		if p, ok := byName[selectNetworks.Selected]; ok { return p }
		return chain.DefaultNetworkPlan()
	}
	selectNetworks.OnChanged = func(name string) {
		if name == customName {
			customNetworks.Show()
			networkDescription.SetText("Введи четыре частные, не пересекающиеся IPv4-сети. Клиенты, Docker и служебная сеть — /24, межсерверная — /30.")
		} else {
			customNetworks.Hide()
			networkDescription.SetText(selectedNetworks().String())
		}
	}
	selectNetworks.SetSelected(presets[0].Name)
	networkCard := standardCard(
		txt("Подсети VPN", 18, snow, true),
		txt("Выбери готовый набор при конфликте с домашней LAN, существующим VPN или Docker. Все четыре подсети согласованы.", 13, secondary, false),
		selectNetworks,
		networkDescription,
		customNetworks,
		txt("Сети проверяются на обоих VPS до установки. Домашнюю сеть и сети других устройств проверь самостоятельно.", 12, secondary, false),
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
			Networks: selectedNetworks(),
		}
		if err := opt.ValidateServers(); err != nil { dialog.ShowError(err, w); return }
		if err := opt.Networks.Validate(); err != nil { dialog.ShowError(err, w); return }

		dialog.NewConfirm("Начать настройку VPN?",
			"На двух ЧИСТЫХ Ubuntu 24.04 установятся официальный Docker AmneziaWG и межсерверный AWG. Настроятся маршруты и firewall. Существующие установки не перезаписываются. SSH не изменяется. Продолжить?",
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
				resultNote.SetText("Настраиваем оба VPS и управляемый AmneziaWG. Пользователи создаются в официальной Amnezia VPN.")
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
					result, err := installer.RunManaged(context.Background(), opt)
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
							resultNote.SetText("На VPS могли остаться частичные изменения. Перед повторной установкой проверь состояние серверов.")
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
						resultNote.SetText(fmt.Sprintf("Входной VPS: %s:%d/UDP\nIP выходного сервера: %s\nПодсети: %s\n\nAmnezia VPN: Self-hosted → Пропустить установку → Управление → Проверить установленные сервисы. После обнаружения amnezia-awg2 создавай пользователей и экспортируй конфиги для Keenetic.", result.EntryIP, result.Port, result.ExitIP, result.Networks.String()))
						addLog("Готово", "amnezia-awg2 на "+result.EntryIP+"; внешний IP: "+result.ExitIP)
						dialog.ShowInformation("Установка завершена", "Двухсерверная цепочка и Docker проверены. В Amnezia VPN открой сервер "+result.EntryIP+" по SSH и обнаружь установленный сервис amnezia-awg2.", w)
					})
				}()
			}, w).Show()
	})

	setupLeft := container.NewVScroll(pad(23,0,25,23,container.NewVBox(
		mainHeader,
		serverCard("Входной сервер", "Принимает подключения через официальную Amnezia VPN", entryIP, entryPort, entryUser, entryPass),
		serverCard("Выходной сервер", "Выпускает трафик в интернет", exitIP, exitPort, exitUser, exitPass),
		networkCard,
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
				routeNode(routerSVG, "router-path", "Amnezia VPN / Keenetic"),
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
		stepRow("3", "Установит нативный AWG для межсерверной связи"),
		stepRow("4", "Поднимет управляемый контейнер amnezia-awg2"),
		stepRow("5", "Настроит Docker, PBR, NAT и kill switch"),
		stepRow("6", "Исправит UDP-ответы для любых клиентов"),
		stepRow("7", "Проверит IP выходного сервера и автозапуск"),
		stepRow("8", "Подготовит управление через Amnezia VPN"),
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
			txt("Управление клиентскими конфигурациями", 18, snow, true),
			txt("После установки добавь сервер в официальную Amnezia VPN, найди сервис amnezia-awg2 и создавай подключения или конфиги Keenetic там.", 14, secondary, false),
			txt("AmneziaWG 3.1 • Docker + нативный межсерверный AWG из GitHub", 14, secondary, false),
			txt("Порты: UDP 55424 (вход), UDP 51820 (выход)", 14, secondary, false),
			txt("Четыре подсети выбираются на экране установки: готовые альтернативы или собственные частные CIDR.", 14, secondary, false),
			txt("Обновления или пересоздание контейнера через официальный клиент требуют повторной диагностики: Docker может изменить сетевые интерфейсы.", 14, secondary, false),
			txt("Установщик не изменяет SSH-аутентификацию и не перезаписывает уже существующие VPN-профили.", 14, secondary, false),
		),
	))

	aboutPage := container.NewVScroll(pad(32,40,32,40,
		standardCard(
			container.NewHBox(imageIcon("about-logo",logoSVG,72),
				container.NewVBox(txt("AWG Chain Installer",28,snow,true),txt("Версия 0.3.1 • Go + Fyne",15,secondary,false))),
			widget.NewSeparator(),
			txt("Автоматическая настройка двухузлового AmneziaWG 3.1",19,snow,true),
			txt("Amnezia VPN / Keenetic → Docker на входном VPS → AWG/IPv6 → выходной VPS",14,secondary,false),
			txt("Встроенные проверки SSH, PBR, NAT, kill switch и Amnezia-managed контейнера.",14,secondary,false),
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
	bottom := container.NewVBox(txt("v0.3.1",13,secondary,false))
	navContent := pad(25,12,20,12,container.NewBorder(nil,bottom,nil,nil,navTop))
	sidebar := container.NewStack(sidebarBackground(),navContent)
	sidebarFixed := container.New(fixedColumn{width:250},sidebar)

	w.SetContent(container.NewBorder(nil,nil,sidebarFixed,nil,contents))
	w.ShowAndRun()
}
