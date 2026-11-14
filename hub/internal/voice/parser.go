package voice

import (
	"strconv"
	"strings"
	"unicode"
)

// accentReplacer strips the accents of the words the parser recognizes, so
// "lâmpada" and "lampada" match the same rule.
var accentReplacer = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)

// wordReplacer turns the separators used in device names into spaces so
// "Ar-condicionado" and "Tomada-PC" tokenize like the spoken phrase.
var wordReplacer = strings.NewReplacer("-", " ", "_", " ", "/", " ", ".", " ")

// normalize lowercases, strips accents and punctuation and collapses
// whitespace. Punctuation matters: Whisper returns "Ligado, Luiz do Quarto."
// and the comma/period glued to words used to defeat every keyword rule.
func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = accentReplacer.Replace(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			return r
		}
		return ' '
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

var (
	onVerbs     = []string{"liga", "ligar", "ligue", "liguem", "acende", "acender", "acenda", "acendo", "ativa", "ativar", "ative", "abre", "abrir", "abra", "aciona", "acionar", "bota", "botar"}
	offVerbs    = []string{"desliga", "desligar", "desligue", "desliguem", "apaga", "apagar", "apague", "apaguem", "fecha", "fechar", "feche", "desativa", "desativar", "desative", "off"}
	sceneVerbs  = []string{"modo", "cena", "cenario", "cenarios"}
	speedWords  = []string{"velocidade", "vel", "grau", "nivel", "giro"}
	tempWords   = []string{"temperatura", "termometro", "termo"}
	degreeWords = []string{"graus", "grau"}

	// Device-type keywords. They are matched against both the transcript and a
	// device's name (see nameHas), so a device named "Tomada-PC" answers
	// "tomada" and "Ar-condicionado" answers "ar".
	lightWords  = []string{"luz", "lampada", "lamp", "abajur", "ilumin", "luminaria", "led"}
	fanWords    = []string{"ventilador", "ventila", "ventilacao", "motor", "helice", "teto", "ventoinha"}
	socketWords = []string{"tomada", "tomadinha", "soquete", "plug", "interruptor"}
	airWords    = []string{"ar", "condicionado", "climatizador", "clima", "split"}
)

// speechFixes corrects high-frequency pt-BR speech-to-text confusions as whole
// words. Whisper often renders "luz" as the name "Luiz" and an imperative as a
// participle ("ligado"), so "Ligado, Luiz do Quarto." still parses as
// "ligar luz do quarto".
var speechFixes = map[string]string{
	"luiz":   "luz",
	"luis":   "luz",
	"luiza":  "luz",
	"luzes":  "luz",
	"ligado": "ligar", "ligada": "ligar", "ligados": "ligar", "ligadas": "ligar",
	"desligado": "desligar", "desligada": "desligar", "desligados": "desligar", "desligadas": "desligar",
	"aceso": "acender", "acesa": "acender", "acesos": "acender", "acesas": "acender",
	"apagado": "apagar", "apagada": "apagar", "apagados": "apagar", "apagadas": "apagar",
}

// repairSpeech rewrites the known confusions. It is idempotent: the targets
// ("luz", "ligar", ...) are not sources.
func repairSpeech(raw string) string {
	fields := strings.Fields(raw)
	for i, w := range fields {
		if fix, ok := speechFixes[w]; ok {
			fields[i] = fix
		}
	}
	return strings.Join(fields, " ")
}

// Parse turns a raw transcript into a Command using pt-BR keyword rules. It
// never fails: an unrecognized phrase yields a Command with an empty Action,
// which the resolver then rejects (and the LLM fallback may still handle).
func Parse(text string) Command {
	return parseRaw(repairSpeech(normalize(text)))
}

// parseRaw runs the keyword rules over an already normalized transcript.
func parseRaw(raw string) Command {
	cmd := Command{Raw: raw}

	// An air conditioner is set in degrees ("põe o ar em 22 graus"), while a fan
	// uses "velocidade 3". The noun decides which reading applies.
	if nounOf(raw) == "air" {
		if n, ok := tempValue(raw); ok {
			cmd.Action = ActionClimate
			cmd.Between = &n
			cmd.Noun = "air"
			return cmd
		}
	}

	if n, ok := speedValue(raw); ok {
		cmd.Action = ActionSpeed
		cmd.Speed = n
		cmd.Noun = nounOf(raw)
		return cmd
	}

	on := hasWord(raw, onVerbs)
	off := hasWord(raw, offVerbs)
	noun := nounOf(raw)

	switch {
	case off:
		cmd.On = false
	case on:
		cmd.On = true
	case hasWord(raw, sceneVerbs):
		cmd.Action = ActionScene
		cmd.Noun = noun
		return cmd
	default:
		cmd.Noun = noun
		return cmd
	}

	if noun == "light" {
		cmd.Action = ActionLight
	} else {
		cmd.Action = ActionPower
	}
	cmd.Noun = noun
	return cmd
}

// nounOf picks the device type of a phrase, most specific first.
func nounOf(raw string) string {
	switch {
	case nameHas(raw, lightWords):
		return "light"
	case nameHas(raw, fanWords):
		return "fan"
	case nameHas(raw, socketWords):
		return "socket"
	case nameHas(raw, airWords):
		return "air"
	}
	return ""
}

// speedValue extracts a fan speed from a phrase ("velocidade 3", "grau dois",
// or a lone number next to a fan noun).
func speedValue(raw string) (int, bool) {
	fields := strings.Fields(raw)
	for i, w := range fields {
		if contains(speedWords, w) {
			for j := i + 1; j < len(fields) && j <= i+3; j++ {
				if n, ok := wordNumber(fields[j]); ok {
					return n, true
				}
			}
		}
	}
	if nounOf(raw) == "fan" {
		nums := []int{}
		for _, w := range fields {
			if n, ok := wordNumber(w); ok {
				nums = append(nums, n)
			}
		}
		if len(nums) == 1 {
			return nums[0], true
		}
	}
	return 0, false
}

// tempValue extracts a target temperature from a phrase that mentions the air
// conditioner: "22 graus", "em 22", "temperatura 22". Values outside the usual
// range are rejected so a stray number is not taken as a temperature.
func tempValue(raw string) (int, bool) {
	if n, ok := untilValue(raw, " ate "); ok {
		return n, true
	}
	fields := strings.Fields(raw)
	for i, w := range fields {
		hasUnit := contains(tempWords, w) || contains(degreeWords, w)
		prevUnit := i > 0 && (contains(tempWords, fields[i-1]) || contains(degreeWords, fields[i-1]))
		if !hasUnit && !prevUnit {
			continue
		}
		for j := i + 1; j < len(fields) && j <= i+3; j++ {
			if n, ok := wordNumber(fields[j]); ok && n >= 16 && n <= 30 {
				return n, true
			}
		}
	}
	if nounOf(raw) == "air" {
		var nums []int
		for _, w := range fields {
			if n, ok := wordNumber(w); ok && n >= 16 && n <= 30 {
				nums = append(nums, n)
			}
		}
		if len(nums) == 1 {
			return nums[0], true
		}
	}
	return 0, false
}

// untilValue reads "de 18 a 24" (or "18 até 22") as the upper limit, which is
// what a thermostat setpoint means.
func untilValue(raw, marker string) (int, bool) {
	i := strings.Index(raw, marker)
	if i < 0 {
		return 0, false
	}
	for _, w := range strings.Fields(raw[i+len(marker):]) {
		if n, ok := wordNumber(w); ok && n >= 16 && n <= 30 {
			return n, true
		}
	}
	return 0, false
}

func wordNumber(w string) (int, bool) {
	switch w {
	case "zero":
		return 0, true
	case "um", "uma":
		return 1, true
	case "dois", "duas":
		return 2, true
	case "tres":
		return 3, true
	case "quatro":
		return 4, true
	case "cinco":
		return 5, true
	case "seis":
		return 6, true
	}
	if w != "" && strings.Trim(w, "0123456789") == "" {
		if n, err := strconv.Atoi(w); err == nil {
			return n, true
		}
	}
	return 0, false
}

// hasWord reports whether raw contains any of the given words as a whole word.
func hasWord(raw string, sets ...[]string) bool {
	fields := strings.Fields(raw)
	for _, set := range sets {
		for _, w := range fields {
			if contains(set, w) {
				return true
			}
		}
	}
	return false
}

// nameHas reports whether text mentions any of the given type keywords. A short
// keyword ("ar", "led") must match a whole word; a longer one ("tomada",
// "ventila") also matches as a substring, so "Tomada-PC" and "Ventilador"
// answer "tomada" and "ventilador". Separators are treated as spaces.
func nameHas(text string, words []string) bool {
	n := wordReplacer.Replace(normalize(text))
	fields := strings.Fields(n)
	for _, w := range words {
		for _, f := range fields {
			if f == w {
				return true
			}
			if len(w) >= 4 && strings.Contains(f, w) {
				return true
			}
		}
	}
	return false
}

// containsWord reports whether needle appears in haystack as a whole word (it
// may be multi-word).
func containsWord(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	return strings.Contains(" "+haystack+" ", " "+needle+" ")
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
