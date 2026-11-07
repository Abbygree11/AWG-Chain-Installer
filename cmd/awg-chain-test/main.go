package main

// Temporary development runner: same chain.Installer as the GUI, without
// importing Fyne, building native desktop binaries, or storing passwords.
import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/Abbygree11/AWG-Chain-Installer/internal/chain"
)

const (
	// Change these two IPs if testing another pair of clean VPS.
	entryIPv4 = "45.88.177.100"
	exitIPv4  = "45.88.163.31"
	entrySSHPort = 22
	exitSSHPort  = 22
	profileName  = "keenetic-test"
)

func readPassword(prompt string) (string, error) {
	fmt.Print(prompt)
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("нужен обычный интерактивный терминал (PowerShell или Terminal)")
	}
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	password := string(b)
	for i := range b { b[i] = 0 }
	if password == "" { return "", fmt.Errorf("пароль не должен быть пустым") }
	return password, nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "\nОШИБКА:", err)
		os.Exit(1)
	}
}

func run() error {
	fmt.Println("AWG Chain / быстрый тест без GUI")
	fmt.Printf("Входной VPS: %s, выходной VPS: %s\n", entryIPv4, exitIPv4)
	fmt.Println("Будут устанавливаться пакеты и настраиваться VPN на ОБОИХ серверах.")
	fmt.Println("Используй только чистые VPS без действующих VPN-профилей.")
	fmt.Print("Продолжить? Введи слово install: ")
	var confirmation string
	if _, err := fmt.Scanln(&confirmation); err != nil { return err }
	if confirmation != "install" { return fmt.Errorf("установка отменена") }

	entryPassword, err := readPassword("SSH-пароль входного VPS: ")
	if err != nil { return err }
	exitPassword, err := readPassword("SSH-пароль выходного VPS: ")
	if err != nil { return err }

	cwd, err := os.Getwd()
	if err != nil { return err }
	dir := filepath.Join(cwd, "profiles")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	installer := chain.Installer{
		Progress: func(percent float64, phase, detail string) {
			fmt.Printf("[%3.0f%%] %s — %s\n", percent*100, phase, detail)
		},
		AskHostKey: func(address, fingerprint string) bool {
			fmt.Printf("\nНовый SSH-ключ сервера %s:\n%s\n", address, fingerprint)
			fmt.Println("Сверь SHA256 с веб-консолью хостинга. Неверный ключ НЕ подтверждай.")
			fmt.Print("Подтвердить? Введи YES: ")
			var answer string
			_, e := fmt.Scanln(&answer)
			return e == nil && strings.TrimSpace(answer) == "YES"
		},
	}

	started := time.Now()
	path, err := installer.Run(ctx, chain.Options{
		Entry: chain.Server{Host:entryIPv4,Port:entrySSHPort,User:"root",Password:entryPassword},
		Exit: chain.Server{Host:exitIPv4,Port:exitSSHPort,User:"root",Password:exitPassword},
		Profile:profileName,
		OutputDir:dir,
	})
	if err != nil { return err }
	fmt.Println("\nГотово:", path)
	fmt.Printf("Выполнено за %s\n", time.Since(started).Round(time.Second))
	fmt.Println("Конфигурационный файл содержит приватный ключ — храни его безопасно.")
	return nil
}
