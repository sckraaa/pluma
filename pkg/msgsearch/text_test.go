package msgsearch

import (
	"reflect"
	"strings"
	"testing"
)

func TestPatterns(t *testing.T) {
	cases := []struct {
		q    string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"Кота", []string{wordStart + "кот"}},
		{"кот", []string{wordStart + "кот"}},
		{"ёлка", []string{wordStart + "[её]лк"}},
		{"купить хлеб", []string{wordStart + "куп", wordStart + "хл[её]б"}},
		{"кот,", []string{wordStart + "кот"}},
		{"#Отпуск", []string{wordStart + `#отпуск` + tagEnd}},
		{"$BTC", []string{wordStart + `\$btc` + tagEnd}},
		{"50%", []string{wordStart + `50%`}},
		{"c++", []string{wordStart + `c\+\+`}},
		{"...", []string{`\.\.\.`}},
		{"😀", []string{"😀"}},
	}
	for _, c := range cases {
		if got := Patterns(c.q); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Patterns(%q) = %q, want %q", c.q, got, c.want)
		}
	}
}

func TestPatternsLimits(t *testing.T) {
	if got := Patterns(strings.Repeat("слово ", 40)); len(got) != maxWords {
		t.Errorf("%d words", len(got))
	}
	long := strings.Repeat("a", 500)
	if got := Patterns(long)[0]; strings.Count(got, "a") != maxWordRunes {
		t.Errorf("long word cut to %d", strings.Count(got, "a"))
	}
}

// The vocabulary sample of snowballstem.org/algorithms/russian/stemmer.html.
func TestStemRussian(t *testing.T) {
	for word, want := range map[string]string{
		"вавиловка":    "вавиловк",
		"вагнера":      "вагнер",
		"вагона":       "вагон",
		"важнее":       "важн",
		"важничал":     "важнича",
		"важности":     "важност",
		"важностью":    "важност",
		"вазах":        "ваз",
		"вакханка":     "вакханк",
		"валандался":   "валанда",
		"валентина":    "валентин",
		"валериановых": "валерианов",
		"валерию":      "валер",
		"валетами":     "валет",
		"валил":        "вал",
		"вальдшнепа":   "вальдшнеп",
		"вальсишку":    "вальсишк",
		"вальтера":     "вальтер",
		"валяется":     "валя",
		"валялась":     "валя",
		"валять":       "валя",
		"вами":         "вам",
		"павловне":     "павловн",
		"падает":       "пада",
		"падчерицей":   "падчериц",
		// The rest are what the search needs of it.
		"кот":  "кот",
		"коты": "кот",
		"ёлки": "елк",
	} {
		if got := stemRussian(word); got != want {
			t.Errorf("stemRussian(%q) = %q, want %q", word, got, want)
		}
	}
}
