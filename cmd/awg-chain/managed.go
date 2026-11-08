package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Abbygree11/AWG-Chain-Installer/internal/chain"
)

// Read-only diagnostic of installed managed and native services. Installation
// is performed by the primary setup page, not by the diagnostic page.
func makeManagedPage(w fyne.Window) fyne.CanvasObject {
	host:=widget.NewEntry()
	host.SetPlaceHolder("Публичный IPv4 промежуточного VPS")
	port:=widget.NewEntry()
	port.SetText("22")
	user:=widget.NewEntry()
	user.SetText("root")
	password:=widget.NewPasswordEntry()
	password.SetPlaceHolder("SSH-пароль")
	checkState:=txt("Не проверено",15,secondary,true)
	results:=widget.NewLabel("Введите SSH-данные для проверки установленных компонентов и таблиц маршрутизации.")
	results.Wrapping=fyne.TextWrapWord

	check:=widget.NewButtonWithIcon("Проверить установленные компоненты",theme.SearchIcon(),nil)
	check.Importance=widget.HighImportance
	check.OnTapped=func(){
		p,err:=strconv.Atoi(strings.TrimSpace(port.Text))
		if err!=nil||p<1||p>65535 {
			dialog.ShowError(fmt.Errorf("Неверный SSH-порт"),w)
			return
		}
		server:=chain.Server{Host:strings.TrimSpace(host.Text),Port:p,User:strings.TrimSpace(user.Text),Password:password.Text}
		if server.Host==""||server.Password==""||server.User!="root" {
			dialog.ShowError(fmt.Errorf("Укажи SSH-адрес, пользователя root и пароль"),w)
			return
		}
		check.Disable()
		checkState.Text="Проверка по SSH…"
		checkState.Color=purple
		checkState.Refresh()
		results.SetText("Читаем только состояние интерфейсов и контейнеров. Изменений на VPS не будет.")
		go func() {
			ask:=func(address,fingerprint string)bool{
				accepted:=make(chan bool,1)
				fyne.Do(func(){
					dialog.NewConfirm("Проверка SSH-ключа",
						fmt.Sprintf("Впервые подключаемся к %s\n\nSHA256:\n%s\n\nСверь отпечаток с консолью VPS у провайдера. Доверять ключу?",address,fingerprint),
						func(ok bool){accepted<-ok},w).Show()
				})
				return <-accepted
			}
			inv,err:=chain.DetectServer(context.Background(),server,ask)
			fyne.Do(func(){
				check.Enable()
				password.SetText("")
				if err!=nil {
					checkState.Text="Не удалось выполнить проверку"
					checkState.Color=purple
					checkState.Refresh()
					results.SetText(err.Error())
					return
				}
				checkState.Text="Обнаружение завершено"
				checkState.Color=mint
				checkState.Refresh()
				var lines []string
				lines=append(lines,inv.Summary())
				if inv.RoutingStatus != "" { lines=append(lines,"\nМаршрутизация:\n"+inv.RoutingStatus) }
				for _,c:=range inv.Components {
					state:="не запущен"
					if c.Running{state="работает"}
					label:="Нативный интерфейс"
					if c.Kind=="managed"{label="Docker-контейнер Amnezia"}
					lines=append(lines,fmt.Sprintf("• %s: %s — %s",label,c.Name,state))
				}
				if inv.Kind==chain.KindStandalone {
					lines=append(lines,"\nНативный AWG не появится в официальном клиенте. Не пытайся установить поверх него контейнер без отдельной проверки портов и маршрутизации.")
				}
				if inv.Kind==chain.KindManaged || inv.Kind==chain.KindMixed {
					lines=append(lines,"\nВ официальной Amnezia VPN: подключись к серверу по SSH → «Пропустить установку» → Управление → «Проверить сервер на ранее установленные сервисы».")
				}
				results.SetText(strings.Join(lines,"\n"))
			})
		}()
	}

	// Existing installations should never be reinstalled for tuning. The
	// read-only benchmark and the optional MSS fix use only entry-VPS SSH.
	perfState := txt("Замеры ещё не проводились", 15, secondary, true)
	perfResult := widget.NewLabel("Проверка покажет время DNS, TCP, TLS и получения первого байта из контейнера. Показатели телефона могут отличаться.")
	perfResult.Wrapping = fyne.TextWrapWord
	readServer := func() (chain.Server, error) {
		p, err := strconv.Atoi(strings.TrimSpace(port.Text))
		if err != nil || p < 1 || p > 65535 {
			return chain.Server{}, fmt.Errorf("Неверный SSH-порт")
		}
		server := chain.Server{Host:strings.TrimSpace(host.Text),Port:p,User:strings.TrimSpace(user.Text),Password:password.Text}
		if server.Host == "" || server.Password == "" || server.User != "root" {
			return chain.Server{}, fmt.Errorf("Укажи SSH-адрес, пользователя root и пароль")
		}
		return server, nil
	}
	askSSH := func(address, fingerprint string) bool {
		accepted:=make(chan bool,1)
		fyne.Do(func() {
			dialog.NewConfirm("Проверка SSH-ключа",
				fmt.Sprintf("Впервые подключаемся к %s\n\nSHA256:\n%s\n\nСверь отпечаток с консолью VPS у провайдера. Доверять ключу?",address,fingerprint),
				func(ok bool){accepted<-ok},w).Show()
		})
		return <-accepted
	}
	var measure, optimize *widget.Button
	perform := func(repair bool) {
		server, err := readServer()
		if err != nil { dialog.ShowError(err,w); return }
		measure.Disable()
		optimize.Disable()
		if repair {
			perfState.Text="Применяем MSS-правила…"
			perfResult.SetText("Проверяем текущую маршрутизацию и обновляем только TCP MSS в firewall.")
		} else {
			perfState.Text="Измеряем DNS, TCP и TLS…"
			perfResult.SetText("Замеры выполняются на входном VPS через выходной сервер. Существующий VPN не изменяется.")
		}
		perfState.Color=purple
		perfState.Refresh()
		go func() {
			var output string
			var err error
			if repair {
				output,err=chain.OptimizeManagedTCP(context.Background(),server,askSSH)
			} else {
				output,err=chain.DiagnoseManagedPerformance(context.Background(),server,askSSH)
			}
			fyne.Do(func(){
				measure.Enable()
				optimize.Enable()
				password.SetText("")
				if err!=nil {
					perfState.Text="Не удалось завершить проверку"
					perfState.Color=purple
					perfResult.SetText(err.Error())
				} else {
					perfState.Text="MSS-оптимизация применена"
					if !repair { perfState.Text="Замеры завершены" }
					perfState.Color=mint
					perfResult.SetText(output)
				}
				perfState.Refresh()
			})
		}()
	}
	measure=widget.NewButtonWithIcon("Замерить открытие сайтов", theme.SearchIcon(), func(){perform(false)})
	measure.Importance=widget.HighImportance
	optimize=widget.NewButton("Настроить TCP MSS для двух туннелей",func(){
		dialog.NewConfirm("Оптимизация TCP на входном VPS",
			"Будет добавлено постоянное ограничение MSS=1160 для новых TCP-соединений внутри VPN-цепочки (в обоих направлениях). Не меняет DNS, ключи или пользователей и не перезапускает VPN. Может помочь при проблемах MTU, но не ускоряет медленный DNS. Продолжить?",
			func(ok bool){if ok{perform(true)}},w).Show()
	})

	docs:=widget.NewHyperlink("Официальная инструкция: как найти установленные сервисы",nil)
	docs.SetURLFromString("https://docs.amnezia.org/ru/documentation/instructions/check-server/")
	pane:=container.NewVScroll(pad(30,35,35,35,container.NewVBox(
		container.NewHBox(imageIcon("managed-symbol",logoSVG,65),
			container.NewVBox(
				txt("Интеграция с Amnezia VPN",28,snow,true),
				txt("Диагностика установленного сервера · только чтение",14,secondary,false),
			)),
		standardCard(
			txt("Проверка промежуточного сервера",21,snow,true),
			txt("Программа определяет нативный AWG и контейнеры Amnezia VPN без изменений в системе.",14,secondary,false),
			container.NewGridWithColumns(2,
				labelledEntry("IPv4 сервера",host),
				labelledEntry("SSH-порт",port),
			),
			container.NewGridWithColumns(2,
				labelledEntry("Пользователь",user),
				labelledEntry("Пароль",password),
			),
			check,
		),
		standardCard(
			txt("Результат диагностики",21,snow,true),
			checkState,
			results,
		),
		standardCard(
			txt("Скорость первого открытия сайта",21,snow,true),
			txt("Два вложенных VPN-туннеля имеют MTU 1280 и 1200. Если первые HTTPS-запросы зависают из-за MTU, TCP MSS=1160 может помочь. Для медленного DNS предусмотрены отдельные замеры.",14,secondary,false),
			measure,
			optimize,
			perfState,
			perfResult,
			txt("Измерение выполняется на сервере, не на телефоне. Сначала сравни результаты до и после применения, не меняй вручную параметры AWG 3.1.",12,secondary,false),
		),
		standardCard(
			txt("Как работает Amnezia-managed",21,snow,true),
			txt("Установка на вкладке «Установка» поднимает amnezia-awg2 на входном VPS и awg-mediator → awg-destination для выхода через второй VPS.",14,secondary,false),
			txt("Пользователи и конфигурации создаются через официальную Amnezia VPN. После обновления или пересоздания контейнера проверь Docker-сеть и PBR повторно: такие изменения пока не восстанавливаются автоматически.",14,secondary,false),
			docs,
		),
	)))
	return pane
}
