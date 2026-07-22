package web

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
)

func TestRoomsPageUsesAccessibleVisualIconPickersAndPreservesLegacyIcon(t *testing.T) {
	t.Parallel()
	router, store := testRouter(t)
	defer store.Close()

	room, err := store.SaveRoom(context.Background(), database.Room{
		Name: "Legacy kitchen", Icon: "kitchen", SortOrder: 10,
	})
	if err != nil {
		t.Fatal(err)
	}

	body := getPageBody(t, router, "/rooms")
	createForm := htmlFragment(t, body, `<form class="form-grid" method="post" action="/rooms">`, "</form>")
	if !strings.Contains(createForm, "<fieldset") || !strings.Contains(createForm, "<legend") || !strings.Contains(visibleText(createForm), "Room icon") {
		t.Fatal("Create Room does not group its visual icon choices with an accessible fieldset and legend")
	}
	for label, emoji := range map[string]string{
		"Home": "🏠", "Kitchen": "🍳", "Bedroom": "🛏", "Office": "💼", "Bathroom": "🛁", "Garage": "🚗",
	} {
		if !strings.Contains(createForm, "<label") || !strings.Contains(visibleText(createForm), label) || !strings.Contains(createForm, emoji) {
			t.Errorf("Create Room icon picker does not include the labeled %s %s option", emoji, label)
		}
	}

	editFormStart := `action="/rooms/` + intString(room.ID) + `/update"`
	editForm := htmlFragment(t, body, editFormStart, "</form>")
	if !hasInputWithAttributes(editForm, `name="icon"`, `value="kitchen"`, "checked") {
		t.Error("existing legacy icon value \"kitchen\" is not preserved as the checked current option")
	}
	if !strings.Contains(visibleText(editForm), "Current icon: kitchen") {
		t.Error("existing legacy icon value is not included in the option's accessible name")
	}

	postForm(t, router, "/rooms", url.Values{"name": {"Office"}, "icon": {"💼"}, "sort_order": {"20"}})
	rooms, err := store.ListRooms(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var office database.Room
	for _, candidate := range rooms {
		if candidate.Name == "Office" {
			office = candidate
			break
		}
	}
	if office.ID == 0 || office.Icon != "💼" {
		t.Fatalf("chosen icon was not saved with the new room: %+v", office)
	}

	postForm(t, router, "/rooms/"+intString(room.ID)+"/update", url.Values{
		"name": {room.Name}, "description": {room.Description}, "icon": {room.Icon}, "sort_order": {"10"},
	})
	updated, err := store.GetRoom(context.Background(), room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Icon != "kitchen" {
		t.Fatalf("submitting an unchanged legacy icon replaced it with %q", updated.Icon)
	}
}

func hasInputWithAttributes(fragment string, attributes ...string) bool {
	for remaining := fragment; ; {
		start := strings.Index(remaining, "<input")
		if start < 0 {
			return false
		}
		remaining = remaining[start:]
		end := strings.Index(remaining, ">")
		if end < 0 {
			return false
		}
		input := remaining[:end+1]
		matches := true
		for _, attribute := range attributes {
			matches = matches && strings.Contains(input, attribute)
		}
		if matches {
			return true
		}
		remaining = remaining[end+1:]
	}
}
