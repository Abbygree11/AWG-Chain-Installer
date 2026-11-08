package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/Abbygree11/AWG-Chain-Installer/internal/chain"
)

func readPassword(prompt string) (string, error) {
	fmt.Print(prompt)
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("нужен интерактивный терминал PowerShell/Terminal")
	}
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	pass := string(b)
	for i := range b {
		b[i] = 0
	}
	if pass == "" {
		return "", errors.New("пароль пуст")
	}
	return pass, nil
}

func askHostKey(address, fingerprint string) bool {
	fmt.Printf("\nНовый SSH-ключ %s:\n%s\n", address, fingerprint)
	fmt.Println("Сверь отпечаток с консолью VPS у провайдера.")
	fmt.Print("Подтверди словом YES: ")
	var answer string
	_, err := fmt.Scanln(&answer)
	return err == nil && answer == "YES"
}

func progress(step, detail string) {
	if len(detail) > 1200 {
		detail = detail[len(detail)-1200:]
	}
	fmt.Printf("\n[%s] %s\n", step, strings.TrimSpace(detail))
}

func run() error {
	entry := flag.String("entry", "45.88.177.100", "IPv4 входного VPS")
	entryPort := flag.Int("entry-port", 22, "SSH-порт входного VPS")
	full := flag.Bool("full", false, "сначала установить обе чистые VPS обычным AWG Chain, затем добавить Amnezia-managed")
	repair := flag.Bool("repair-replies", false, "починить ответы UDP на уже установленном amnezia-awg2 без переустановки")
	exit := flag.String("exit", "", "IPv4 выходного VPS, обязательно с -full")
	exitPort := flag.Int("exit-port", 22, "SSH-порт выходного VPS")
	flag.Parse()

	ip, err := netip.ParseAddr(*entry)
	if err != nil || !ip.Is4() || !ip.IsGlobalUnicast() {
		return fmt.Errorf("нужен публичный IPv4 входного VPS, получено %q", *entry)
	}
	if *entryPort < 1 || *entryPort > 65535 {
		return errors.New("неверный SSH-порт входного VPS")
	}
	if *full && *repair { return errors.New("режимы -full и -repair-replies несовместимы") }
	if *full {
		exitIP, e := netip.ParseAddr(*exit)
		if e != nil || !exitIP.Is4() || !exitIP.IsGlobalUnicast() || *exit == *entry {
			return errors.New("для -full укажи другой публичный IPv4 через -exit")
		}
		if *exitPort < 1 || *exitPort > 65535 {
			return errors.New("неверный SSH-порт выходного VPS")
		}
	}

	fmt.Println("AWG Chain / Amnezia-managed CLI (ЭКСПЕРИМЕНТ)")
	fmt.Println("Вход:", *entry)
	if *repair {
		fmt.Println("Режим repair: поправим только исходящие UDP-ответы существующего amnezia-awg2")
	} else if *full {
		fmt.Println("Полная установка с нуля: два VPS, затем managed Docker")
	} else {
		fmt.Println("Режим attach: существующие awg-mediator и awg-destination должны работать")
	}
	fmt.Println("Проверяется и изменяется сеть VPS. Действующий awg-client не удаляется.")
	if *repair { fmt.Println("Ключи, peers и существующая конфигурация AWG не изменяются.") }
	fmt.Print("Продолжить? Введи install: ")
	var confirm string
	if _, err := fmt.Scanln(&confirm); err != nil {
		return err
	}
	if confirm != "install" {
		return errors.New("отменено")
	}
	entryPass, err := readPassword("SSH-пароль входного VPS: ")
	if err != nil {
		return err
	}
	var exitPass string
	if *full {
		exitPass, err = readPassword("SSH-пароль выходного VPS: ")
		if err != nil {
			return err
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	server := chain.Server{Host: *entry, Port: *entryPort, User: "root", Password: entryPass}
	if *full {
		fmt.Println("\n=== Установка рабочей нативной цепочки ===")
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		inst := chain.Installer{
			AskHostKey: askHostKey,
			Progress: func(p float64, title, detail string) {
				fmt.Printf("[%.0f%%] %s: %s\n", 100*p, title, detail)
			},
		}
		result, err := inst.Run(ctx, chain.Options{
			Entry:     server,
			Exit:      chain.Server{Host: *exit, Port: *exitPort, User: "root", Password: exitPass},
			Profile:   "temporary-native",
			OutputDir: filepath.Join(wd, "profiles"),
		})
		if err != nil {
			return fmt.Errorf("нативная цепочка: %w", err)
		}
		fmt.Println("Нативная цепочка готова. Резервный профиль:", result)
	}

	remote, err := chain.Dial(ctx, server, askHostKey)
	if err != nil {
		return err
	}
	defer remote.Close()
	if *repair {
		if err := (chain.ManagedAttacher{Progress: progress}).RepairReplies(ctx, remote); err != nil { return err }
		fmt.Println("\nПравило ответного UDP-маршрута установлено без перезапуска контейнера. Проверь подключение клиентов.")
		return nil
	}
	fmt.Println("\n=== Добавляем официальный управляемый AmneziaWG 3.1 ===")
	started := time.Now()
	attacher := chain.ManagedAttacher{Progress: progress}
	if err := attacher.Run(ctx, remote); err != nil {
		return err
	}
	fmt.Printf("\nMANAGED CHAIN OK (%s)\n", time.Since(started).Round(time.Second))
	fmt.Println("Проверь в Amnezia VPN: Self-hosted → Пропустить установку → Управление → Найти установленные сервисы.")
	fmt.Println("Ожидаем amnezia-awg2, UDP 55424. После импорта создай тестового клиента и проверь внешний IP.")
	fmt.Println("Не удаляй awg-client до проверки работы официального клиента.")
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "\nОШИБКА:", err)
		os.Exit(1)
	}
}
