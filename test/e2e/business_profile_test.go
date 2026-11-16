//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/catalog/stickerfile"
	"github.com/teamgram/teamgram-server/pkg/mtclient"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The Business part of a profile (task 105): work hours with open_now as
// the server tells it, a location and an intro with a sticker. Another
// user sees them in users.getFullUser; the other device of the account
// learns each change by updateUser.
func TestBusinessProfile(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, laptop)
	aliceForBob := inputUserOf(ctx, t, bob, alice)

	full := func() *mtproto.UserFull {
		t.Helper()
		o, err := bob.c.Invoke(ctx, &mtproto.TLUsersGetFullUser{
			Constructor: bob.c.Constructor(mtproto.Predicate_users_getFullUser),
			Id:          aliceForBob,
		})
		if err != nil {
			t.Fatalf("users.getFullUser: %v", err)
		}
		return o.(interface {
			To_Users_UserFull() *mtproto.Users_UserFull
		}).To_Users_UserFull().GetFullUser()
	}
	// waitUpdateUser waits on the laptop for updateUser about Alice.
	waitUpdateUser := func(what string) {
		t.Helper()
		if _, err := laptop.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
			for _, u := range us.GetUpdates() {
				if u.GetPredicateName() == mtproto.Predicate_updateUser && u.GetUserId() == alice.self.GetId() {
					return true
				}
			}
			return false
		}); err != nil {
			t.Fatalf("%s: no updateUser on the other device: %v", what, err)
		}
	}
	ok := func(method string, o mtproto.TLObject, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		if b, _ := o.(interface{ To_Bool() *mtproto.Bool }); b == nil || !mtproto.FromBool(b.To_Bool()) {
			t.Fatalf("%s: %v, want boolTrue", method, o)
		}
	}
	setHours := func(timezone string, open ...int32) (mtproto.TLObject, error) {
		var weekly []*mtproto.BusinessWeeklyOpen
		for i := 0; i+1 < len(open); i += 2 {
			weekly = append(weekly, &mtproto.BusinessWeeklyOpen{
				Constructor: alice.c.Constructor(mtproto.Predicate_businessWeeklyOpen),
				StartMinute: open[i],
				EndMinute:   open[i+1],
			})
		}
		return alice.c.Invoke(ctx, &mtproto.TLAccountUpdateBusinessWorkHours{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_updateBusinessWorkHours),
			BusinessWorkHours: &mtproto.BusinessWorkHours{
				Constructor: alice.c.Constructor(mtproto.Predicate_businessWorkHours),
				OpenNow:     true, // ignored: only the server sets open_now
				TimezoneId:  timezone,
				WeeklyOpen:  weekly,
			},
		})
	}

	if f := full(); f.GetBusinessWorkHours() != nil || f.GetBusinessLocation() != nil || f.GetBusinessIntro() != nil {
		t.Fatalf("a new account has Business: %v", f)
	}

	// Open all week, sent as Monday 09:00 to Monday 09:00 a week later:
	// kept as the whole week, open now.
	o, err := setHours("Europe/Berlin", 540, business.MinutesPerWeek+540)
	ok("account.updateBusinessWorkHours", o, err)
	waitUpdateUser("the work hours")
	h := full().GetBusinessWorkHours()
	if !h.GetOpenNow() || h.GetTimezoneId() != "Europe/Berlin" || len(h.GetWeeklyOpen()) != 1 ||
		h.GetWeeklyOpen()[0].GetStartMinute() != 0 || h.GetWeeklyOpen()[0].GetEndMinute() != business.MinutesPerWeek {
		t.Errorf("the work hours open all week: %v", h)
	}

	// One hour, ten hours from now in London: closed now.
	london, err := business.LoadTimezone("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	start := (business.MinuteOfWeek(time.Now().In(london)) + 600) % business.MinutesPerWeek
	o, err = setHours("Europe/London", start, start+60)
	ok("account.updateBusinessWorkHours", o, err)
	if h = full().GetBusinessWorkHours(); h == nil || h.GetOpenNow() || h.GetTimezoneId() != "Europe/London" {
		t.Errorf("the work hours closed now: %v", h)
	}

	// The errors of core.telegram.org/method/account.updateBusinessWorkHours.
	if _, err = setHours("Mars/Olympus", 540, 1080); !mtclient.IsRPCError(err, "TIMEZONE_INVALID") {
		t.Errorf("an unknown time zone: %v, want TIMEZONE_INVALID", err)
	}
	if _, err = setHours("Europe/London", 100, 100); !mtclient.IsRPCError(err, "BUSINESS_WORK_HOURS_PERIOD_INVALID") {
		t.Errorf("an empty interval: %v, want BUSINESS_WORK_HOURS_PERIOD_INVALID", err)
	}
	if _, err = setHours("Europe/London"); !mtclient.IsRPCError(err, "BUSINESS_WORK_HOURS_EMPTY") {
		t.Errorf("no intervals: %v, want BUSINESS_WORK_HOURS_EMPTY", err)
	}

	// The location with a point.
	o, err = alice.c.Invoke(ctx, &mtproto.TLAccountUpdateBusinessLocation{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateBusinessLocation),
		GeoPoint: &mtproto.InputGeoPoint{
			Constructor: alice.c.Constructor(mtproto.Predicate_inputGeoPoint),
			Lat:         52.5170365,
			Long:        13.3888599,
		},
		Address: wrapperspb.String("Unter den Linden 1"),
	})
	ok("account.updateBusinessLocation", o, err)
	waitUpdateUser("the location")
	if l := full().GetBusinessLocation(); l.GetAddress() != "Unter den Linden 1" || l.GetGeoPoint().GetLat() != 52.5170365 || l.GetGeoPoint().GetLong() != 13.3888599 {
		t.Errorf("the location: %v", l)
	}

	// The intro with a sticker.
	sticker := uploadStickerFile(ctx, t, alice, stickerfile.SampleWebp(512, 512), "image/webp", "sticker.webp")
	o, err = alice.c.Invoke(ctx, &mtproto.TLAccountUpdateBusinessIntro{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateBusinessIntro),
		Intro: &mtproto.InputBusinessIntro{
			Constructor: alice.c.Constructor(mtproto.Predicate_inputBusinessIntro),
			Title:       "Welcome",
			Description: "Ask me anything",
			Sticker:     sticker,
		},
	})
	ok("account.updateBusinessIntro", o, err)
	waitUpdateUser("the intro")
	if i := full().GetBusinessIntro(); i.GetTitle() != "Welcome" || i.GetDescription() != "Ask me anything" || i.GetSticker().GetId() != sticker.GetId() {
		t.Errorf("the intro: %v", i)
	}

	// Removed one by one: a call without the flag.
	o, err = alice.c.Invoke(ctx, &mtproto.TLAccountUpdateBusinessWorkHours{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateBusinessWorkHours),
	})
	ok("account.updateBusinessWorkHours", o, err)
	o, err = alice.c.Invoke(ctx, &mtproto.TLAccountUpdateBusinessLocation{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateBusinessLocation),
	})
	ok("account.updateBusinessLocation", o, err)
	if f := full(); f.GetBusinessWorkHours() != nil || f.GetBusinessLocation() != nil || f.GetBusinessIntro().GetTitle() != "Welcome" {
		t.Errorf("after removing the hours and the location: %v", f)
	}
	o, err = alice.c.Invoke(ctx, &mtproto.TLAccountUpdateBusinessIntro{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateBusinessIntro),
	})
	ok("account.updateBusinessIntro", o, err)
	if f := full(); f.GetBusinessIntro() != nil {
		t.Errorf("after removing the intro: %v", f.GetBusinessIntro())
	}
}

// TestBusinessProfileLimits: what the Business profile refuses (task 105):
// an address longer than 96 characters ("max 96 UTF-8 characters",
// core.telegram.org/api/business) and a point off the map; an intro title
// and description longer than intro_title_length_limit (32) and
// intro_description_length_limit (70) of the app config. A location with
// an address and inputGeoPointEmpty, as tdesktop sends one without a point
// (data/business/data_business_info.cpp:144-160), has no point.
func TestBusinessProfileLimits(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	aliceForBob := inputUserOf(ctx, t, bob, alice)

	location := func(address string, point *mtproto.InputGeoPoint) error {
		_, err := alice.c.Invoke(ctx, &mtproto.TLAccountUpdateBusinessLocation{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_updateBusinessLocation),
			GeoPoint:    point,
			Address:     wrapperspb.String(address),
		})
		return err
	}
	intro := func(title, description string) error {
		_, err := alice.c.Invoke(ctx, &mtproto.TLAccountUpdateBusinessIntro{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_updateBusinessIntro),
			Intro: &mtproto.InputBusinessIntro{
				Constructor: alice.c.Constructor(mtproto.Predicate_inputBusinessIntro),
				Title:       title,
				Description: description,
				Sticker:     &mtproto.InputDocument{Constructor: alice.c.Constructor(mtproto.Predicate_inputDocumentEmpty)},
			},
		})
		return err
	}
	noPoint := &mtproto.InputGeoPoint{Constructor: alice.c.Constructor(mtproto.Predicate_inputGeoPointEmpty)}

	// 96 characters, Cyrillic ones of two bytes each among them, is the
	// longest address.
	longest := strings.Repeat("Улица", 19) + "1"
	if err := location(longest+"2", noPoint); !mtclient.IsRPCError(err, "ADDRESS_INVALID") {
		t.Errorf("an address of 97 characters: %v, want ADDRESS_INVALID", err)
	}
	if err := location("Main St 1", &mtproto.InputGeoPoint{Constructor: alice.c.Constructor(mtproto.Predicate_inputGeoPoint), Lat: 91, Long: 13}); !mtclient.IsRPCError(err, "GEO_POINT_INVALID") {
		t.Errorf("a latitude of 91: %v, want GEO_POINT_INVALID", err)
	}
	if err := location(longest, noPoint); err != nil {
		t.Fatalf("an address of 96 characters: %v", err)
	}
	if l := bizFullUser(ctx, t, bob, aliceForBob).GetBusinessLocation(); l.GetAddress() != longest || l.GetGeoPoint() != nil {
		t.Errorf("the location without a point: %v", l)
	}

	title, description := strings.Repeat("t", 32), strings.Repeat("d", 70)
	if err := intro(title+"t", description); !mtclient.IsRPCError(err, "TITLE_INVALID") {
		t.Errorf("a title of 33 characters: %v, want TITLE_INVALID", err)
	}
	if err := intro(title, description+"d"); !mtclient.IsRPCError(err, "DESCRIPTION_INVALID") {
		t.Errorf("a description of 71 characters: %v, want DESCRIPTION_INVALID", err)
	}
	if err := intro(title, description); err != nil {
		t.Fatalf("an intro at the limits: %v", err)
	}
	if i := bizFullUser(ctx, t, bob, aliceForBob).GetBusinessIntro(); i.GetTitle() != title || i.GetDescription() != description || i.GetSticker() != nil {
		t.Errorf("the intro at the limits, without a sticker: %v", i)
	}
}
