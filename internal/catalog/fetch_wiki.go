package catalog

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/erdaboss99/genshin-tools/internal/game"
)

const (
	wikiMenuCharacters = "2"
	wikiMenuWeapons    = "4"
	wikiMenuArtifacts  = "5"
	wikiPageSize       = 50
	wikiArtifactList   = "artifact_list"
	wikiReferer        = "https://wiki.hoyolab.com/"
	wikiLanguage       = "en-us"
	travelerName       = "Traveler"
)

// wikiCorrections replaces piece names the wiki has wrong, by set and slot.
var wikiCorrections = map[string]map[game.Slot]string{
	"Obsidian Codex": {game.SlotFlower: "Reckoning of the Xenogenic"},
}

// wikiListRequest asks for one page of a wiki menu.
type wikiListRequest struct {
	Filters  []string `json:"filters"`
	MenuID   string   `json:"menu_id"`
	PageNum  int      `json:"page_num"`
	PageSize int      `json:"page_size"`
	UseES    bool     `json:"use_es"`
}

// wikiListItem is one entry of a wiki menu.
type wikiListItem struct {
	ID   string `json:"entry_page_id"`
	Name string `json:"name"`
}

// wikiListData is the payload of a menu page.
type wikiListData struct {
	List  []wikiListItem `json:"list"`
	Total string         `json:"total"`
}

// wikiListResponse is the response to a wikiListRequest.
type wikiListResponse struct {
	Retcode int          `json:"retcode"`
	Message string       `json:"message"`
	Data    wikiListData `json:"data"`
}

// wikiComponent is one block of a wiki page. Data holds JSON encoded as a string.
type wikiComponent struct {
	ID   string `json:"component_id"`
	Data string `json:"data"`
}

// wikiModule is a group of components on a wiki page.
type wikiModule struct {
	Components []wikiComponent `json:"components"`
}

// wikiPage is one wiki page.
type wikiPage struct {
	Modules []wikiModule `json:"modules"`
}

// wikiEntryData is the payload of a page request.
type wikiEntryData struct {
	Page wikiPage `json:"page"`
}

// wikiEntryResponse is the response to a page request.
type wikiEntryResponse struct {
	Data wikiEntryData `json:"data"`
}

// wikiPiece is one entry of the wiki's artifact_list component. The object keys
// (flower_of_life, ...) are shuffled for recent sets, while position is correct,
// so position is what gets used.
type wikiPiece struct {
	Title    string `json:"title"`
	Position string `json:"position"`
}

// wikiSet maps the artifact_list component's keys to the pieces of one set.
type wikiSet map[string]wikiPiece

func wikiRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", wikiReferer)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Rpc-Language", wikiLanguage)
	return req, nil
}

func wikiList(ctx context.Context, client *http.Client, src Source, menu string) ([]wikiListItem, error) {
	var items []wikiListItem
	for page := 1; ; page++ {
		payload, err := json.Marshal(wikiListRequest{
			Filters: []string{}, MenuID: menu, PageNum: page, PageSize: wikiPageSize, UseES: true,
		})
		if err != nil {
			return nil, err
		}
		req, err := wikiRequest(ctx, http.MethodPost, src.WikiListURL, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		var resp wikiListResponse
		if err := doJSON(client, req, &resp); err != nil {
			return nil, err
		}
		if resp.Retcode != 0 {
			return nil, fmt.Errorf("wiki list: retcode %d: %s", resp.Retcode, resp.Message)
		}
		items = append(items, resp.Data.List...)
		total, err := strconv.Atoi(resp.Data.Total)
		if err != nil {
			return nil, fmt.Errorf("wiki list: bad total %q: %w", resp.Data.Total, err)
		}
		if len(items) >= total || len(resp.Data.List) == 0 {
			return items, nil
		}
	}
}

func fetchCharacters(ctx context.Context, client *http.Client, src Source) ([]string, map[string]ConstellationBoosts, error) {
	items, err := wikiList(ctx, client, src, wikiMenuCharacters)
	if err != nil {
		return nil, nil, err
	}
	names := []string{travelerName}
	boosts := make(map[string]ConstellationBoosts, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		names = append(names, name)
		page, err := fetchCharacterPage(ctx, client, src, item.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("character %q: %w", name, err)
		}
		if b, ok := page.boosts(); ok {
			boosts[name] = b
		}
	}
	slices.Sort(names)
	return slices.Compact(names), boosts, nil
}

// wikiCharacter holds the parts of a character page that the boosts come from.
type wikiCharacter struct {
	// talents are the talent titles in page order: the normal attack, the skill, an
	// alternate sprint for a few characters, then the burst, then the passives.
	talents []string
	// constellations are the constellation descriptions, in order.
	constellations []string
}

type wikiTalentList struct {
	List []struct {
		Title string `json:"title"`
	} `json:"list"`
}

type wikiSummaryList struct {
	List []struct {
		Desc string `json:"desc"`
	} `json:"list"`
}

const (
	wikiTalentComponent        = "talent"
	wikiConstellationComponent = "summaryList"
	// Constellations 3 and 5 raise talent levels, at these positions.
	wikiC3, wikiC5 = 2, 4
)

var htmlTag = regexp.MustCompile(`<[^>]*>`)

func fetchCharacterPage(ctx context.Context, client *http.Client, src Source, id string) (wikiCharacter, error) {
	var page wikiCharacter
	req, err := wikiRequest(ctx, http.MethodGet, src.WikiEntryURL+"?entry_page_id="+id, nil)
	if err != nil {
		return page, err
	}
	var resp wikiEntryResponse
	if err := doJSON(client, req, &resp); err != nil {
		return page, err
	}
	for _, module := range resp.Data.Page.Modules {
		for _, component := range module.Components {
			// Characters that aren't out yet have pages whose components are empty.
			if strings.TrimSpace(component.Data) == "" {
				continue
			}
			switch component.ID {
			case wikiTalentComponent:
				var talents wikiTalentList
				if err := json.Unmarshal([]byte(component.Data), &talents); err != nil {
					return page, fmt.Errorf("%s: %w", wikiTalentComponent, err)
				}
				for _, t := range talents.List {
					page.talents = append(page.talents, strings.TrimSpace(t.Title))
				}
			case wikiConstellationComponent:
				var list wikiSummaryList
				if err := json.Unmarshal([]byte(component.Data), &list); err != nil {
					return page, fmt.Errorf("%s: %w", wikiConstellationComponent, err)
				}
				for _, c := range list.List {
					page.constellations = append(page.constellations, html.UnescapeString(htmlTag.ReplaceAllString(c.Desc, "")))
				}
			}
		}
	}
	return page, nil
}

// boosts works out what constellations 3 and 5 raise. Their descriptions name the
// talent, and the page's talent order says which one it is: the normal attack is
// first, the skill second, and the burst third or, for characters with an alternate
// sprint, fourth. Names are compared ignoring case, as the wiki isn't consistent.
func (p wikiCharacter) boosts() (ConstellationBoosts, bool) {
	if len(p.talents) < 4 || len(p.constellations) <= wikiC5 {
		return ConstellationBoosts{}, false
	}
	candidates := []struct {
		kind  game.TalentKind
		title string
	}{
		{game.TalentAuto, p.talents[0]}, {game.TalentSkill, p.talents[1]}, {game.TalentBurst, p.talents[2]}, {game.TalentBurst, p.talents[3]},
	}
	kind := func(desc string) game.TalentKind {
		desc = strings.ToLower(desc)
		if !strings.Contains(desc, "level of") {
			return ""
		}
		// The longest matching title wins, in case one title contains another.
		best, bestLen := game.TalentKind(""), 0
		for _, c := range candidates {
			title := strings.ToLower(c.title)
			if title != "" && strings.Contains(desc, title) && len(title) > bestLen {
				best, bestLen = c.kind, len(title)
			}
		}
		return best
	}
	b := ConstellationBoosts{C3: kind(p.constellations[wikiC3]), C5: kind(p.constellations[wikiC5])}
	if b.C3 == "" || b.C5 == "" || b.C3 == b.C5 {
		return ConstellationBoosts{}, false
	}
	return b, true
}

func fetchWeapons(ctx context.Context, client *http.Client, src Source) ([]string, error) {
	items, err := wikiList(ctx, client, src, wikiMenuWeapons)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		if name := strings.TrimSpace(item.Name); name != "" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names), nil
}

func fetchPieces(ctx context.Context, client *http.Client, src Source) ([]Piece, error) {
	sets, err := wikiList(ctx, client, src, wikiMenuArtifacts)
	if err != nil {
		return nil, err
	}
	var pieces []Piece
	for _, set := range sets {
		entries, err := fetchSet(ctx, client, src, set.ID)
		if err != nil {
			return nil, fmt.Errorf("set %q: %w", set.Name, err)
		}
		pieces = append(pieces, piecesFromSet(strings.TrimSpace(set.Name), entries)...)
	}
	return cleanPieces(pieces), nil
}

func fetchSet(ctx context.Context, client *http.Client, src Source, id string) (wikiSet, error) {
	req, err := wikiRequest(ctx, http.MethodGet, src.WikiEntryURL+"?entry_page_id="+id, nil)
	if err != nil {
		return nil, err
	}
	var resp wikiEntryResponse
	if err := doJSON(client, req, &resp); err != nil {
		return nil, err
	}
	for _, module := range resp.Data.Page.Modules {
		for _, component := range module.Components {
			if component.ID != wikiArtifactList {
				continue
			}
			var entries wikiSet
			if err := json.Unmarshal([]byte(component.Data), &entries); err != nil {
				return nil, fmt.Errorf("%s: %w", wikiArtifactList, err)
			}
			return entries, nil
		}
	}
	return nil, errors.New("page has no artifact_list component")
}

func piecesFromSet(set string, entries wikiSet) []Piece {
	var pieces []Piece
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Title)
		if name == "" {
			continue
		}
		slot, _ := game.SlotFromLabel(entry.Position)
		if fixed, ok := wikiCorrections[set][slot]; ok {
			name = fixed
		}
		pieces = append(pieces, Piece{Name: name, Set: set, Slot: slot})
	}
	return pieces
}

// cleanPieces sorts pieces and clears the slot of any name listed under several
// slots, since such an entry cannot be trusted to identify a slot.
func cleanPieces(pieces []Piece) []Piece {
	slices.SortFunc(pieces, func(a, b Piece) int {
		return cmp.Or(cmp.Compare(a.Set, b.Set), cmp.Compare(a.Name, b.Name), cmp.Compare(a.Slot, b.Slot))
	})
	out := pieces[:0]
	for _, p := range pieces {
		if n := len(out); n > 0 && out[n-1].Name == p.Name && out[n-1].Set == p.Set {
			out[n-1].Slot = ""
			continue
		}
		out = append(out, p)
	}
	return out
}
