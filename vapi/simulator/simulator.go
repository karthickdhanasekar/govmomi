/*
Copyright (c) 2018 VMware, Inc. All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package simulator

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/vmware/govmomi/vapi/tags"
	vim "github.com/vmware/govmomi/vim25/types"
)

const (
	Path              = "/rest/com/vmware"
	SessionCookieName = "vmware-api-session-id"
)

type handler struct {
	*http.ServeMux
	sync.Mutex
	Category    map[string]*tags.Category
	Tag         map[string]*tags.Tag
	Association map[string]map[tags.AssociatedObject]bool
}

// New creates a vAPI simulator.
func New(u *url.URL, settings []vim.BaseOptionValue) (string, http.Handler) {
	s := &handler{
		ServeMux:    http.NewServeMux(),
		Category:    make(map[string]*tags.Category),
		Tag:         make(map[string]*tags.Tag),
		Association: make(map[string]map[tags.AssociatedObject]bool),
	}

	handlers := []struct {
		p string
		m http.HandlerFunc
	}{
		{"cis/session", s.session},
		{"cis/tagging/category", s.category},
		{"cis/tagging/category/", s.categoryID},
		{"cis/tagging/tag", s.tag},
		{"cis/tagging/tag/", s.tagID},
		{"cis/tagging/tag-association", s.association},
	}

	for i := range handlers {
		h := handlers[i]
		s.HandleFunc(Path+"/"+h.p, func(w http.ResponseWriter, r *http.Request) {
			s.Lock()
			defer s.Unlock()

			h.m(w, r)
		})
	}

	return Path + "/", s
}

// ok responds with http.StatusOK and json encodes val if given.
func (s *handler) ok(w http.ResponseWriter, val ...interface{}) {
	w.WriteHeader(http.StatusOK)

	if len(val) == 0 {
		return
	}

	err := json.NewEncoder(w).Encode(struct {
		Value interface{} `json:"value,omitempty"`
	}{
		val[0],
	})

	if err != nil {
		log.Panic(err)
	}
}

func (s *handler) fail(w http.ResponseWriter, kind string) {
	w.WriteHeader(http.StatusBadRequest)

	err := json.NewEncoder(w).Encode(struct {
		Type  string `json:"type"`
		Value struct {
			Messages []string `json:"messages,omitempty"`
		} `json:"value,omitempty"`
	}{
		Type: kind,
	})

	if err != nil {
		log.Panic(err)
	}
}

// ServeHTTP handles vAPI requests.
func (s *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost, http.MethodDelete, http.MethodGet, http.MethodPatch:
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	h, _ := s.Handler(r)
	h.ServeHTTP(w, r)
}

func (s *handler) decode(r *http.Request, w http.ResponseWriter, val interface{}) bool {
	defer r.Body.Close()
	err := json.NewDecoder(r.Body).Decode(val)
	if err != nil {
		log.Printf("%s %s: %s", r.Method, r.RequestURI, err)
		w.WriteHeader(http.StatusBadRequest)
		return false
	}
	return true
}

func (s *handler) session(w http.ResponseWriter, r *http.Request) {
	var id string

	switch r.Method {
	case http.MethodPost:
		id = uuid.New().String()
		// TODO: save session
		http.SetCookie(w, &http.Cookie{
			Name:  SessionCookieName,
			Value: id,
		})
		s.ok(w)
	case http.MethodDelete:
		// TODO: delete session
		s.ok(w)
	case http.MethodGet:
		// TODO: test is session is valid
		s.ok(w, id)
	}
}

func (s *handler) action(r *http.Request) string {
	return r.URL.Query().Get("~action")
}

func (s *handler) id(r *http.Request) string {
	id := path.Base(r.URL.Path)
	return strings.TrimPrefix(id, "id:")
}

func newID(kind string) string {
	return fmt.Sprintf("urn:vmomi:InventoryService%s:%s:GLOBAL", kind, uuid.New().String())
}

func (s *handler) category(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var spec tags.CategoryCreateSpec
		if s.decode(r, w, &spec) {
			for _, category := range s.Category {
				if category.Name == spec.CreateSpec.Name {
					s.fail(w, "com.vmware.vapi.std.errors.already_exists")
					return
				}
			}
			id := newID("Category")
			spec.CreateSpec.ID = id
			s.Category[id] = &spec.CreateSpec
			s.ok(w, id)
		}
	case http.MethodGet:
		var ids []string
		for id := range s.Category {
			ids = append(ids, id)
		}

		s.ok(w, ids)
	}
}

func (s *handler) categoryID(w http.ResponseWriter, r *http.Request) {
	id := s.id(r)

	o, ok := s.Category[id]
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodDelete:
		delete(s.Category, id)
		s.ok(w)
	case http.MethodPatch:
		var spec tags.CategoryUpdateSpec
		if s.decode(r, w, &spec) {
			o.Update(&spec.UpdateSpec)
			s.ok(w)
		}
	case http.MethodGet:
		s.ok(w, o)
	}
}

func (s *handler) tag(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var spec tags.TagCreateSpec
		if s.decode(r, w, &spec) {
			for _, tag := range s.Tag {
				if tag.Name == spec.CreateSpec.Name {
					s.fail(w, "com.vmware.vapi.std.errors.already_exists")
					return
				}
			}
			id := newID("Tag")
			spec.CreateSpec.ID = id
			s.Tag[id] = &spec.CreateSpec
			s.Association[id] = make(map[tags.AssociatedObject]bool)
			s.ok(w, id)
		}
	case http.MethodGet:
		var ids []string
		for id := range s.Tag {
			ids = append(ids, id)
		}
		s.ok(w, ids)
	}
}

func (s *handler) tagID(w http.ResponseWriter, r *http.Request) {
	id := s.id(r)

	switch s.action(r) {
	case "list-tags-for-category":
		var ids []string
		for _, tag := range s.Tag {
			if tag.CategoryID == id {
				ids = append(ids, tag.ID)
			}
		}
		s.ok(w, ids)
		return
	}

	o, ok := s.Tag[id]
	if !ok {
		log.Printf("tag not found: %s", id)
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodDelete:
		delete(s.Tag, id)
		delete(s.Association, id)
		s.ok(w)
	case http.MethodPatch:
		var spec tags.TagUpdateSpec
		if s.decode(r, w, &spec) {
			o.Update(&spec.UpdateSpec)
			s.ok(w)
		}
	case http.MethodGet:
		s.ok(w, o)
	}
}

func (s *handler) association(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var spec tags.TagAssociationSpec
	if !s.decode(r, w, &spec) {
		return
	}

	if spec.TagID != "" {
		if _, exists := s.Association[spec.TagID]; !exists {
			log.Printf("association tag not found: %s", spec.TagID)
			http.NotFound(w, r)
			return
		}
	}

	switch s.action(r) {
	case "attach":
		s.Association[spec.TagID][*spec.ObjectID] = true
		s.ok(w)
	case "detach":
		delete(s.Association[spec.TagID], *spec.ObjectID)
		s.ok(w)
	case "list-attached-tags":
		var ids []string
		for id, objs := range s.Association {
			if objs[*spec.ObjectID] {
				ids = append(ids, id)
			}
		}
		s.ok(w, ids)
	case "list-attached-objects":
		var ids []tags.AssociatedObject
		for id := range s.Association[spec.TagID] {
			ids = append(ids, id)
		}
		s.ok(w, ids)
	}
}
