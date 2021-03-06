// Copyright 2016 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/tsuru/tsuru/app"
	"github.com/tsuru/tsuru/auth"
	"github.com/tsuru/tsuru/errors"
	"github.com/tsuru/tsuru/event"
	"github.com/tsuru/tsuru/permission"
)

// title: user quota
// path: /users/{email}/quota
// method: GET
// produce: application/json
// responses:
//   200: OK
//   401: Unauthorized
//   404: User not found
func getUserQuota(w http.ResponseWriter, r *http.Request, t auth.Token) error {
	allowed := permission.Check(t, permission.PermUserUpdateQuota)
	if !allowed {
		return permission.ErrUnauthorized
	}
	email := r.URL.Query().Get(":email")
	user, err := auth.GetUserByEmail(email)
	if err == auth.ErrUserNotFound {
		return &errors.HTTP{
			Code:    http.StatusNotFound,
			Message: err.Error(),
		}
	}
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(user.Quota)
}

// title: update user quota
// path: /users/{email}/quota
// method: PUT
// consume: application/x-www-form-urlencoded
// responses:
//   200: Quota updated
//   400: Invalid data
//   401: Unauthorized
//   404: User not found
func changeUserQuota(w http.ResponseWriter, r *http.Request, t auth.Token) (err error) {
	r.ParseForm()
	allowed := permission.Check(t, permission.PermUserUpdateQuota)
	if !allowed {
		return permission.ErrUnauthorized
	}
	email := r.URL.Query().Get(":email")
	user, err := auth.GetUserByEmail(email)
	if err == auth.ErrUserNotFound {
		return &errors.HTTP{
			Code:    http.StatusNotFound,
			Message: err.Error(),
		}
	} else if err != nil {
		return err
	}
	evt, err := event.New(&event.Opts{
		Target:     event.Target{Type: event.TargetTypeUser, Value: email},
		Kind:       permission.PermUserUpdateQuota,
		Owner:      t,
		CustomData: formToEvents(r.Form),
	})
	if err != nil {
		return err
	}
	defer func() { evt.Done(err) }()
	limit, err := strconv.Atoi(r.FormValue("limit"))
	if err != nil {
		return &errors.HTTP{
			Code:    http.StatusBadRequest,
			Message: "Invalid limit",
		}
	}
	return auth.ChangeQuota(user, limit)
}

// title: application quota
// path: /apps/{appname}/quota
// method: GET
// produce: application/json
// responses:
//   200: OK
//   401: Unauthorized
//   404: Application not found
func getAppQuota(w http.ResponseWriter, r *http.Request, t auth.Token) error {
	a, err := getAppFromContext(r.URL.Query().Get(":appname"), r)
	if err != nil {
		return err
	}
	canRead := permission.Check(t, permission.PermAppRead,
		append(permission.Contexts(permission.CtxTeam, a.Teams),
			permission.Context(permission.CtxApp, a.Name),
			permission.Context(permission.CtxPool, a.Pool),
		)...,
	)
	if !canRead {
		return permission.ErrUnauthorized
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(a.Quota)
}

// title: update application quota
// path: /apps/{appname}/quota
// method: PUT
// consume: application/x-www-form-urlencoded
// responses:
//   200: Quota updated
//   400: Invalid data
//   401: Unauthorized
//   404: Application not found
func changeAppQuota(w http.ResponseWriter, r *http.Request, t auth.Token) (err error) {
	r.ParseForm()
	appName := r.URL.Query().Get(":appname")
	a, err := getAppFromContext(appName, r)
	if err != nil {
		return err
	}
	allowed := permission.Check(t, permission.PermAppAdminQuota,
		append(permission.Contexts(permission.CtxTeam, a.Teams),
			permission.Context(permission.CtxApp, a.Name),
			permission.Context(permission.CtxPool, a.Pool),
		)...,
	)
	if !allowed {
		return permission.ErrUnauthorized
	}
	evt, err := event.New(&event.Opts{
		Target:     event.Target{Type: event.TargetTypeApp, Value: appName},
		Kind:       permission.PermAppAdminQuota,
		Owner:      t,
		CustomData: formToEvents(r.Form),
	})
	if err != nil {
		return err
	}
	defer func() { evt.Done(err) }()
	limit, err := strconv.Atoi(r.FormValue("limit"))
	if err != nil {
		return &errors.HTTP{
			Code:    http.StatusBadRequest,
			Message: "Invalid limit",
		}
	}
	return app.ChangeQuota(&a, limit)
}
