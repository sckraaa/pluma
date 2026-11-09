// Copyright 2026 The Pluma Authors
//  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"golang.org/x/term"
	"rsc.io/qr"
)

// quietZone is the blank border of the QR code, in modules.
const quietZone = 2

// renderQR draws text as a QR code with half-block characters, two modules
// to a character: light modules are drawn lit, so it reads on a dark
// terminal, where a scanner expects a dark code on a light field.
func renderQR(text string) (string, error) {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return "", err
	}
	dark := func(x, y int) bool {
		if x < 0 || y < 0 || x >= code.Size || y >= code.Size {
			return false
		}
		return code.Black(x, y)
	}
	var b strings.Builder
	for y := -quietZone; y < code.Size+quietZone; y += 2 {
		for x := -quietZone; x < code.Size+quietZone; x++ {
			switch top, bottom := dark(x, y), dark(x, y+1); {
			case !top && !bottom:
				b.WriteString("█")
			case !top && bottom:
				b.WriteString("▀")
			case top && !bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

// showQR replaces the screen with the current code: TDLib renews the link
// frequently, and only the latest one works.
func showQR(link string) {
	drawn, err := renderQR(link)
	if err != nil {
		log.Printf("codes: draw the QR code: %v", err)
		return
	}
	fmt.Print("\033[2J\033[H")
	fmt.Print(drawn)
	fmt.Println("Telegram на телефоне: Настройки → Устройства → Подключить устройство, отсканируйте код.")
	fmt.Println("Код обновляется сам; сканируйте последний. Выход: Ctrl+C.")
}

// readPassword reads the two-step verification password without echo.
func readPassword(hint string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("the password has to be typed on a terminal: run with -it")
	}
	if hint != "" {
		fmt.Printf("Подсказка: %s\n", hint)
	}
	fmt.Print("Пароль двухэтапной аутентификации: ")
	password, err := term.ReadPassword(fd)
	fmt.Println()
	if err != nil {
		return "", err
	}
	return string(password), nil
}
