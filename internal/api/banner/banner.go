package banner

import "fmt"

const (
	orange = "\033[38;5;214m"
	purple = "\033[38;5;93m"
	green  = "\033[92m"
	reset  = "\033[0m"

	version = "1.0.0"
)

var artLines = []string{
	"",
	" ███████████                                      ██████     █████",
	"░░███░░░░░███                                   ███░░░░███  ░░███",
	" ░███    ░███   ██████    ████████    ██████   ███    ░░███  ░███",
	" ░██████████   ░░░░░███  ░░███░░███  ███░░███ ░███     ░███  ░███",
	" ░███░░░░░░     ███████   ░███ ░███ ░███ ░███ ░███   ██░███  ░███",
	" ░███          ███░░███   ░███ ░███ ░███ ░███ ░░███ ░░████   ░███      █",
	" █████        ░░████████  ░███████  ░░██████   ░░░██████░██  ███████████",
	"░░░░░          ░░░░░░░░   ░███░░░    ░░░░░░      ░░░░░░ ░░  ░░░░░░░░░░░",
	"                          ░███",
	"                          █████",
	"                         ░░░░░",
}

func PrintBanner() {
	for _, line := range artLines {
		runes := []rune(line)

		if len(runes) > 45 {
			fmt.Println(
				orange +
					string(runes[:45]) +
					purple +
					string(runes[45:]) +
					orange +
					reset,
			)
			continue
		}

		fmt.Println(orange + line + reset)
	}
}

func PrintInfo() {
	fmt.Printf("  %s🡺  %sPapo%sQL%s - v%s%s\n",
		purple,
		orange,
		purple,
		reset,
		version,
		reset,
	)

	fmt.Printf("  %s🡺  %sDeveloped by: Pedro Borges (@pedroborgesdev)%s\n",
		orange,
		reset,
		reset,
	)

	fmt.Printf("  %s🡺  %sGitHub: https://github.com/pedroborgesdev/PapoQL%s\n\n",
		purple,
		reset,
		reset,
	)
}

func PrintStartup() {
	PrintBanner()
	PrintInfo()
}
