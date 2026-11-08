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

package polls

import (
	"slices"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"

	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Checklist limits: core.telegram.org/api/config (todo_items_max 30,
// todo_title_length_max 255, todo_item_length_max 200).
const (
	TodoItemsMax = 30
	TodoTitleMax = 255
	TodoItemMax  = 200
)

// Errors of api/todo and method/messages.appendTodoList.
var (
	ErrTodoItemsEmpty   = status.Error(mtproto.ErrBadRequest, "TODO_ITEMS_EMPTY")
	ErrTodoItemsTooMuch = status.Error(mtproto.ErrBadRequest, "TODO_ITEMS_TOO_MUCH")
	ErrTodoItemDup      = status.Error(mtproto.ErrBadRequest, "TODO_ITEM_DUPLICATE")
	ErrTodoNotModified  = status.Error(mtproto.ErrBadRequest, "TODO_NOT_MODIFIED")
)

// CheckTodo checks a list as sent: items present and within the limits, ids
// positive and unique (api/todo). The title and the texts must be valid.
func CheckTodo(list *mtproto.TodoList) error {
	if list == nil || len(list.GetList()) == 0 {
		return ErrTodoItemsEmpty
	}
	if err := checkItems(list.GetList(), list.GetList()[:0]); err != nil {
		return err
	}
	return checkLen(list.GetTitle().GetText(), TodoTitleMax)
}

func checkLen(text string, limit int) error {
	if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > limit {
		return mtproto.ErrMessageEmpty
	}
	return nil
}

// checkItems checks items against the existing ones.
func checkItems(items, existing []*mtproto.TodoItem) error {
	if len(items)+len(existing) > TodoItemsMax {
		return ErrTodoItemsTooMuch
	}
	seen := make(map[int32]bool, len(items)+len(existing))
	for _, i := range existing {
		seen[i.GetId()] = true
	}
	for _, i := range items {
		if i.GetId() <= 0 || seen[i.GetId()] {
			return ErrTodoItemDup
		}
		seen[i.GetId()] = true
		if err := checkLen(i.GetTitle().GetText(), TodoItemMax); err != nil {
			return err
		}
	}
	return nil
}

// TodoMedia is messageMediaToDo, without completions when there are none.
func TodoMedia(list *mtproto.TodoList, completions []*mtproto.TodoCompletion) *mtproto.MessageMedia {
	return mtproto.MakeTLMessageMediaToDo(&mtproto.MessageMedia{Todo: list, Completions: completions}).To_MessageMedia()
}

// Toggle returns completions with the items of completed marked done by peer
// at date and those of incompleted undone; the ids of list only, and the
// ids it actually changed. A completed item keeps its first completer.
func Toggle(list *mtproto.TodoList, completions []*mtproto.TodoCompletion, completed, incompleted []int32, by *mtproto.Peer, date int32) (out []*mtproto.TodoCompletion, doneIds, undoneIds []int32) {
	item := func(id int32) bool {
		return slices.ContainsFunc(list.GetList(), func(i *mtproto.TodoItem) bool { return i.GetId() == id })
	}
	done := func(id int32) bool {
		return slices.ContainsFunc(completions, func(c *mtproto.TodoCompletion) bool { return c.GetId() == id })
	}
	out = slices.Clone(completions)
	for _, id := range incompleted {
		if item(id) && done(id) {
			out = slices.DeleteFunc(out, func(c *mtproto.TodoCompletion) bool { return c.GetId() == id })
			undoneIds = append(undoneIds, id)
		}
	}
	for _, id := range completed {
		if item(id) && !done(id) && !slices.Contains(doneIds, id) {
			out = append(out, mtproto.MakeTLTodoCompletion(&mtproto.TodoCompletion{
				Id:               id,
				CompletedBy_PEER: by,
				Date:             date,
			}).To_TodoCompletion())
			doneIds = append(doneIds, id)
		}
	}
	return out, doneIds, undoneIds
}

// AppendItems returns list with items added at its end; the errors of
// messages.appendTodoList.
func AppendItems(list *mtproto.TodoList, items []*mtproto.TodoItem) (*mtproto.TodoList, error) {
	if len(items) == 0 {
		return nil, ErrTodoNotModified
	}
	if err := checkItems(items, list.GetList()); err != nil {
		return nil, err
	}
	out := proto.Clone(list).(*mtproto.TodoList)
	out.List = append(out.List, items...)
	return out, nil
}

// EditList returns next, what an edit of the list by its author leaves
// (messages.editMessage with inputMediaTodo, api/todo: removed items lose
// their completion, the others keep it).
func EditList(next *mtproto.TodoList, completions []*mtproto.TodoCompletion) (*mtproto.TodoList, []*mtproto.TodoCompletion, error) {
	if err := CheckTodo(next); err != nil {
		return nil, nil, err
	}
	kept := slices.DeleteFunc(slices.Clone(completions), func(c *mtproto.TodoCompletion) bool {
		return !slices.ContainsFunc(next.GetList(), func(i *mtproto.TodoItem) bool { return i.GetId() == c.GetId() })
	})
	return next, kept, nil
}
