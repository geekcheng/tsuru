// Copyright 2016 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hipache

import (
	"net/url"
	"reflect"
	"sync"
	"testing"

	"github.com/tsuru/config"
	"github.com/tsuru/tsuru/db"
	"github.com/tsuru/tsuru/db/dbtest"
	"github.com/tsuru/tsuru/redis"
	"github.com/tsuru/tsuru/router"
	"github.com/tsuru/tsuru/router/routertest"
	"gopkg.in/check.v1"
)

func Test(t *testing.T) {
	check.TestingT(t)
}

type S struct {
	conn   *db.Storage
	prefix string
}

var _ = check.Suite(&S{})

func init() {
	base := &S{}
	suite := &routertest.RouterSuite{
		SetUpSuiteFunc:   base.SetUpSuite,
		TearDownTestFunc: base.TearDownTest,
	}
	suite.SetUpTestFunc = func(c *check.C) {
		config.Set("database:name", "router_generic_hipache_tests")
		config.Set("routers:generic_hipache:redis-server", "127.0.0.1:6379")
		config.Set("routers:generic_hipache:redis-db", 3)
		config.Set("routers:generic_hipache:domain", "hipache.router")
		base.prefix = "routers:generic_hipache"
		base.SetUpTest(c)
		suite.Router = &hipacheRouter{prefix: base.prefix}
	}
	check.Suite(suite)
}

func clearConnCache() {
	redisClientsMut.Lock()
	defer redisClientsMut.Unlock()
	for _, c := range redisClients {
		c.Close()
	}
	redisClients = map[string]redis.Client{}
}

func clearRedisKeys(keysPattern string, conn redis.Client, c *check.C) {
	keys, err := conn.Keys(keysPattern).Result()
	c.Assert(err, check.IsNil)
	for _, key := range keys {
		conn.Del(key)
	}
}

func (s *S) SetUpSuite(c *check.C) {
	config.Set("hipache:domain", "golang.org")
	config.Set("database:url", "127.0.0.1:27017")
	config.Set("database:name", "router_hipache_tests")
}

func (s *S) SetUpTest(c *check.C) {
	clearConnCache()
	config.Set("hipache:redis-server", "127.0.0.1:6379")
	var err error
	s.conn, err = db.Conn()
	c.Assert(err, check.IsNil)
	if s.prefix == "" {
		s.prefix = "hipache"
	}
	dbtest.ClearAllCollections(s.conn.Collection("router_hipache_tests").Database)
	rtest := hipacheRouter{prefix: s.prefix}
	conn, err := rtest.connect()
	c.Assert(err, check.IsNil)
	clearRedisKeys("frontend*", conn, c)
	clearRedisKeys("cname*", conn, c)
	clearRedisKeys("*.com", conn, c)
}

func (s *S) TearDownTest(c *check.C) {
	s.conn.Close()
}

func (s *S) TestStressRace(c *check.C) {
	rtest := hipacheRouter{prefix: "hipache"}
	wg := sync.WaitGroup{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			routerConn, err := rtest.connect()
			c.Check(err, check.IsNil)
			err = routerConn.Ping().Err()
			c.Check(err, check.IsNil)
		}()
	}
	wg.Wait()
}

func (s *S) TestConnect(c *check.C) {
	rtest := hipacheRouter{prefix: "hipache"}
	got, err := rtest.connect()
	c.Assert(err, check.IsNil)
	c.Assert(got, check.NotNil)
	err = got.Ping().Err()
	c.Assert(err, check.IsNil)
}

func (s *S) TestConnectCachesConnectionAcrossInstances(c *check.C) {
	rtest := hipacheRouter{prefix: "hipache"}
	got1, err := rtest.connect()
	c.Assert(err, check.IsNil)
	c.Assert(got1, check.NotNil)
	got2, err := rtest.connect()
	c.Assert(err, check.IsNil)
	c.Assert(got2, check.NotNil)
	rtest = hipacheRouter{prefix: "hipache"}
	got3, err := rtest.connect()
	c.Assert(err, check.IsNil)
	c.Assert(got3, check.NotNil)
	rtest = hipacheRouter{prefix: "hipache2"}
	other, err := rtest.connect()
	c.Assert(err, check.IsNil)
	c.Assert(other, check.NotNil)
	c.Assert(reflect.ValueOf(got1).Pointer(), check.Equals, reflect.ValueOf(got2).Pointer())
	c.Assert(reflect.ValueOf(got1).Pointer(), check.Equals, reflect.ValueOf(got3).Pointer())
	c.Assert(reflect.ValueOf(got1).Pointer(), check.Not(check.Equals), reflect.ValueOf(other).Pointer())
}

func (s *S) TestConnectWithPassword(c *check.C) {
	config.Set("hipache:redis-password", "123456")
	defer config.Unset("hipache:redis-password")
	clearConnCache()
	rtest := hipacheRouter{prefix: "hipache"}
	got, err := rtest.connect()
	c.Assert(err, check.ErrorMatches, "ERR Client sent AUTH, but no password is set")
	c.Assert(got, check.IsNil)
}

func (s *S) TestConnectWhenConnIsNilAndCannotConnect(c *check.C) {
	config.Set("hipache:redis-server", "127.0.0.1:6380")
	defer config.Unset("hipache:redis-server")
	clearConnCache()
	rtest := hipacheRouter{prefix: "hipache"}
	got, err := rtest.connect()
	c.Assert(err, check.NotNil)
	c.Assert(got, check.IsNil)
}

func (s *S) TestShouldBeRegistered(c *check.C) {
	r, err := router.Get("hipache")
	c.Assert(err, check.IsNil)
	_, ok := r.(*hipacheRouter)
	c.Assert(ok, check.Equals, true)
}

func (s *S) TestShouldBeRegisteredAsPlanb(c *check.C) {
	config.Set("routers:myplanb:type", "planb")
	defer config.Unset("routers:myplanb:type")
	r, err := router.Get("myplanb")
	c.Assert(err, check.IsNil)
	_, ok := r.(*hipacheRouter)
	c.Assert(ok, check.Equals, true)
}

func (s *S) TestShouldBeRegisteredAllowingPrefixes(c *check.C) {
	config.Set("routers:inst1:type", "hipache")
	config.Set("routers:inst2:type", "hipache")
	defer config.Unset("routers:inst1:type")
	defer config.Unset("routers:inst2:type")
	got1, err := router.Get("inst1")
	c.Assert(err, check.IsNil)
	got2, err := router.Get("inst2")
	c.Assert(err, check.IsNil)
	r1, ok := got1.(*hipacheRouter)
	c.Assert(ok, check.Equals, true)
	c.Assert(r1.prefix, check.Equals, "routers:inst1")
	r2, ok := got2.(*hipacheRouter)
	c.Assert(ok, check.Equals, true)
	c.Assert(r2.prefix, check.Equals, "routers:inst2")
}

func (s *S) TestAddBackend(c *check.C) {
	router := hipacheRouter{prefix: "hipache"}
	err := router.AddBackend("tip")
	c.Assert(err, check.IsNil)
	defer router.RemoveBackend("tip")
	conn, err := router.connect()
	c.Assert(err, check.IsNil)
	backends, err := conn.LLen("frontend:tip.golang.org").Result()
	c.Assert(err, check.IsNil)
	c.Assert(int64(1), check.Equals, backends)
}

func (s *S) TestRemoveBackend(c *check.C) {
	router := hipacheRouter{prefix: "hipache"}
	err := router.AddBackend("tip")
	c.Assert(err, check.IsNil)
	err = router.RemoveBackend("tip")
	c.Assert(err, check.IsNil)
	conn, err := router.connect()
	c.Assert(err, check.IsNil)
	backends, err := conn.LLen("frontend:tip.golang.org").Result()
	c.Assert(err, check.IsNil)
	c.Assert(int64(0), check.Equals, backends)
}

func (s *S) TestRemoveBackendAlsoRemovesRelatedCNameBackendAndControlRecord(c *check.C) {
	router := hipacheRouter{prefix: "hipache"}
	err := router.AddBackend("tip")
	c.Assert(err, check.IsNil)
	err = router.SetCName("mycname.com", "tip")
	c.Assert(err, check.IsNil)
	conn, err := router.connect()
	c.Assert(err, check.IsNil)
	cnames, err := conn.LLen("cname:tip").Result()
	c.Assert(err, check.IsNil)
	c.Assert(int64(1), check.Equals, cnames)
	err = router.RemoveBackend("tip")
	c.Assert(err, check.IsNil)
	cnames, err = conn.LLen("cname:tip").Result()
	c.Assert(err, check.IsNil)
	c.Assert(int64(0), check.Equals, cnames)
}

func (s *S) TestAddRouteWithoutAssemblingFrontend(c *check.C) {
	router := hipacheRouter{prefix: "hipache"}
	err := router.addRoute("test.com", "10.10.10.10")
	c.Assert(err, check.IsNil)
	conn, err := router.connect()
	c.Assert(err, check.IsNil)
	routes, err := conn.LRange("test.com", 0, -1).Result()
	c.Assert(err, check.IsNil)
	c.Assert(routes, check.DeepEquals, []string{"10.10.10.10"})
}

func (s *S) TestAddRoute(c *check.C) {
	router := hipacheRouter{prefix: "hipache"}
	err := router.AddBackend("tip")
	c.Assert(err, check.IsNil)
	defer router.RemoveBackend("tip")
	addr, _ := url.Parse("http://10.10.10.10:8080")
	err = router.AddRoute("tip", addr)
	c.Assert(err, check.IsNil)
	defer router.RemoveRoute("tip", addr)
	conn, err := router.connect()
	c.Assert(err, check.IsNil)
	routes, err := conn.LRange("frontend:tip.golang.org", 0, -1).Result()
	c.Assert(err, check.IsNil)
	c.Assert(routes, check.DeepEquals, []string{"tip", "http://10.10.10.10:8080"})
}

func (s *S) TestAddRouteNoDomainConfigured(c *check.C) {
	r := hipacheRouter{prefix: "hipache"}
	err := r.AddBackend("tip")
	c.Assert(err, check.IsNil)
	defer r.RemoveBackend("tip")
	old, _ := config.Get("hipache:domain")
	defer config.Set("hipache:domain", old)
	config.Unset("hipache:domain")
	addr, _ := url.Parse("http://10.10.10.10:8080")
	err = r.AddRoute("tip", addr)
	c.Assert(err, check.NotNil)
	defer r.RemoveRoute("tip", addr)
	e, ok := err.(*router.RouterError)
	c.Assert(ok, check.Equals, true)
	c.Assert(e.Op, check.Equals, "add")
}

func (s *S) TestAddRouteConnectFailure(c *check.C) {
	r := hipacheRouter{prefix: "hipache"}
	err := r.AddBackend("tip")
	c.Assert(err, check.IsNil)
	defer r.RemoveBackend("tip")
	config.Set("hipache:redis-server", "127.0.0.1:6380")
	defer config.Unset("hipache:redis-server")
	clearConnCache()
	r2 := hipacheRouter{prefix: "hipache"}
	addr, _ := url.Parse("http://www.tsuru.io")
	err = r2.AddRoute("tip", addr)
	c.Assert(err, check.NotNil)
	defer r2.RemoveRoute("tip", addr)
	e, ok := err.(*router.RouterError)
	c.Assert(ok, check.Equals, true)
	c.Assert(e.Op, check.Equals, "routes")
}

func (s *S) TestAddRouteAlsoUpdatesCNameRecordsWhenExists(c *check.C) {
	router := hipacheRouter{prefix: "hipache"}
	err := router.AddBackend("tip")
	c.Assert(err, check.IsNil)
	defer router.RemoveBackend("tip")
	addr, _ := url.Parse("http://10.10.10.10:8080")
	err = router.AddRoute("tip", addr)
	c.Assert(err, check.IsNil)
	defer router.RemoveRoute("tip", addr)
	err = router.SetCName("mycname.com", "tip")
	c.Assert(err, check.IsNil)
	conn, err := router.connect()
	c.Assert(err, check.IsNil)
	cnameRoutes, err := conn.LLen("frontend:mycname.com").Result()
	c.Assert(err, check.IsNil)
	c.Assert(int64(2), check.Equals, cnameRoutes)
	addr, _ = url.Parse("http://10.10.10.11:8080")
	err = router.AddRoute("tip", addr)
	c.Assert(err, check.IsNil)
	defer router.RemoveRoute("tip", addr)
	cnameRoutes, err = conn.LLen("frontend:mycname.com").Result()
	c.Assert(err, check.IsNil)
	c.Assert(int64(3), check.Equals, cnameRoutes)
}

func (s *S) TestRemoveRoute(c *check.C) {
	router := hipacheRouter{prefix: "hipache"}
	err := router.AddBackend("tip")
	c.Assert(err, check.IsNil)
	addr, _ := url.Parse("http://10.10.10.10")
	err = router.AddRoute("tip", addr)
	c.Assert(err, check.IsNil)
	err = router.RemoveRoute("tip", addr)
	c.Assert(err, check.IsNil)
	err = router.RemoveBackend("tip")
	c.Assert(err, check.IsNil)
	conn, err := router.connect()
	c.Assert(err, check.IsNil)
	routes, err := conn.LLen("frontend:tip.golang.org").Result()
	c.Assert(err, check.IsNil)
	c.Assert(int64(0), check.Equals, routes)
}

func (s *S) TestRemoveRouteNoDomainConfigured(c *check.C) {
	r := hipacheRouter{prefix: "hipache"}
	err := r.AddBackend("tip")
	c.Assert(err, check.IsNil)
	defer r.RemoveBackend("tip")
	old, _ := config.Get("hipache:domain")
	defer config.Set("hipache:domain", old)
	config.Unset("hipache:domain")
	addr, _ := url.Parse("http://tip.golang.org")
	err = r.RemoveRoute("tip", addr)
	c.Assert(err, check.NotNil)
	e, ok := err.(*router.RouterError)
	c.Assert(ok, check.Equals, true)
	c.Assert(e.Op, check.Equals, "remove")
}

func (s *S) TestRemoveRouteConnectFailure(c *check.C) {
	r := hipacheRouter{prefix: "hipache"}
	err := r.AddBackend("tip")
	c.Assert(err, check.IsNil)
	defer r.RemoveBackend("tip")
	config.Set("hipache:redis-server", "127.0.0.1:6380")
	defer config.Unset("hipache:redis-server")
	clearConnCache()
	r2 := hipacheRouter{prefix: "hipache"}
	addr, _ := url.Parse("http://tip.golang.org")
	err = r2.RemoveRoute("tip", addr)
	c.Assert(err, check.NotNil)
	e, ok := err.(*router.RouterError)
	c.Assert(ok, check.Equals, true)
	c.Assert(e.Op, check.Equals, "remove")
}

func (s *S) TestRemoveRouteAlsoRemovesRespectiveCNameRecord(c *check.C) {
	router := hipacheRouter{prefix: "hipache"}
	err := router.AddBackend("tip")
	c.Assert(err, check.IsNil)
	defer router.RemoveBackend("tip")
	addr, _ := url.Parse("http://10.10.10.10")
	err = router.AddRoute("tip", addr)
	c.Assert(err, check.IsNil)
	err = router.SetCName("test.com", "tip")
	c.Assert(err, check.IsNil)
	err = router.RemoveRoute("tip", addr)
	c.Assert(err, check.IsNil)
	conn, err := router.connect()
	c.Assert(err, check.IsNil)
	cnames, err := conn.LLen("cname:test.com").Result()
	c.Assert(err, check.IsNil)
	c.Assert(cnames, check.Equals, int64(0))
}

func (s *S) TestHealthCheck(c *check.C) {
	router := hipacheRouter{prefix: "hipache"}
	c.Assert(router.HealthCheck(), check.IsNil)
}

func (s *S) TestHealthCheckFailure(c *check.C) {
	config.Set("super-hipache:redis-server", "localhost:6739")
	defer config.Unset("super-hipache:redis-server")
	router := hipacheRouter{prefix: "super-hipache"}
	err := router.HealthCheck()
	c.Assert(err, check.NotNil)
}

func (s *S) TestGetCNames(c *check.C) {
	router := hipacheRouter{prefix: "hipache"}
	err := router.AddBackend("myapp")
	c.Assert(err, check.IsNil)
	defer router.RemoveBackend("myapp")
	err = router.SetCName("coolcname.com", "myapp")
	c.Assert(err, check.IsNil)
	cnames, err := router.getCNames("myapp")
	c.Assert(err, check.IsNil)
	c.Assert(cnames, check.DeepEquals, []string{"coolcname.com"})
}

func (s *S) TestGetCNameIgnoresErrNil(c *check.C) {
	router := hipacheRouter{prefix: "hipache"}
	cnames, err := router.getCNames("myapp")
	c.Assert(err, check.IsNil)
	c.Assert(cnames, check.DeepEquals, []string{})
}

func (s *S) TestSetCName(c *check.C) {
	router := hipacheRouter{prefix: "hipache"}
	err := router.AddBackend("myapp")
	c.Assert(err, check.IsNil)
	defer router.RemoveBackend("myapp")
	err = router.SetCName("myapp.com", "myapp")
	c.Assert(err, check.IsNil)
}

func (s *S) TestSetCNameWithPreviousRoutes(c *check.C) {
	router := hipacheRouter{prefix: "hipache"}
	err := router.AddBackend("myapp")
	c.Assert(err, check.IsNil)
	defer router.RemoveBackend("myapp")
	addr1, _ := url.Parse("http://10.10.10.10")
	err = router.AddRoute("myapp", addr1)
	c.Assert(err, check.IsNil)
	defer router.RemoveRoute("myapp", addr1)
	addr2, _ := url.Parse("http://10.10.10.11")
	err = router.AddRoute("myapp", addr2)
	c.Assert(err, check.IsNil)
	defer router.RemoveRoute("myapp", addr2)
	err = router.SetCName("mycname.com", "myapp")
	c.Assert(err, check.IsNil)
	conn, err := router.connect()
	c.Assert(err, check.IsNil)
	cnameRoutes, err := conn.LRange("frontend:mycname.com", 0, -1).Result()
	c.Assert(err, check.IsNil)
	c.Assert([]string{"myapp", addr1.String(), addr2.String()}, check.DeepEquals, cnameRoutes)
}

func (s *S) TestSetCNameTwiceFixInconsistencies(c *check.C) {
	r := hipacheRouter{prefix: "hipache"}
	err := r.AddBackend("myapp")
	c.Assert(err, check.IsNil)
	defer r.RemoveBackend("myapp")
	addr1, _ := url.Parse("http://10.10.10.10")
	err = r.AddRoute("myapp", addr1)
	c.Assert(err, check.IsNil)
	defer r.RemoveRoute("myapp", addr1)
	addr2, _ := url.Parse("http://10.10.10.11")
	err = r.AddRoute("myapp", addr2)
	c.Assert(err, check.IsNil)
	defer r.RemoveRoute("myapp", addr2)
	expected := []string{"myapp", addr1.String(), addr2.String()}
	err = r.SetCName("mycname.com", "myapp")
	c.Assert(err, check.IsNil)
	conn, err := r.connect()
	c.Assert(err, check.IsNil)
	cnameRoutes, err := conn.LRange("frontend:mycname.com", 0, -1).Result()
	c.Assert(err, check.IsNil)
	c.Assert(cnameRoutes, check.DeepEquals, expected)
	err = conn.RPush("frontend:mycname.com", "http://invalid.addr:1234").Err()
	c.Assert(err, check.IsNil)
	err = r.SetCName("mycname.com", "myapp")
	c.Assert(err, check.Equals, router.ErrCNameExists)
	cnameRoutes, err = conn.LRange("frontend:mycname.com", 0, -1).Result()
	c.Assert(err, check.IsNil)
	c.Assert(cnameRoutes, check.DeepEquals, expected)
	err = conn.LRem("frontend:mycname.com", 1, "http://10.10.10.10").Err()
	c.Assert(err, check.IsNil)
	err = r.SetCName("mycname.com", "myapp")
	c.Assert(err, check.Equals, router.ErrCNameExists)
	cnameRoutes, err = conn.LRange("frontend:mycname.com", 0, -1).Result()
	c.Assert(err, check.IsNil)
	c.Assert(cnameRoutes, check.DeepEquals, expected)
	err = r.SetCName("mycname.com", "myapp")
	c.Assert(err, check.Equals, router.ErrCNameExists)
	cnameRoutes, err = conn.LRange("frontend:mycname.com", 0, -1).Result()
	c.Assert(err, check.IsNil)
	c.Assert(cnameRoutes, check.DeepEquals, expected)
}

func (s *S) TestSetCNameShouldRecordAppAndCNameOnRedis(c *check.C) {
	router := hipacheRouter{prefix: "hipache"}
	err := router.AddBackend("myapp")
	c.Assert(err, check.IsNil)
	defer router.RemoveBackend("myapp")
	err = router.SetCName("mycname.com", "myapp")
	c.Assert(err, check.IsNil)
	conn, err := router.connect()
	c.Assert(err, check.IsNil)
	cname, err := conn.LRange("cname:myapp", 0, -1).Result()
	c.Assert(err, check.IsNil)
	c.Assert([]string{"mycname.com"}, check.DeepEquals, cname)
}

func (s *S) TestSetCNameSetsMultipleCNames(c *check.C) {
	router := hipacheRouter{prefix: "hipache"}
	err := router.AddBackend("myapp")
	c.Assert(err, check.IsNil)
	defer router.RemoveBackend("myapp")
	addr, _ := url.Parse("http://10.10.10.10")
	err = router.AddRoute("myapp", addr)
	c.Assert(err, check.IsNil)
	defer router.RemoveRoute("myapp", addr)
	err = router.SetCName("mycname.com", "myapp")
	c.Assert(err, check.IsNil)
	err = router.SetCName("myothercname.com", "myapp")
	conn, err := router.connect()
	c.Assert(err, check.IsNil)
	cname, err := conn.LRange("frontend:mycname.com", 0, -1).Result()
	c.Assert(err, check.IsNil)
	c.Assert([]string{"myapp", addr.String()}, check.DeepEquals, cname)
	cname, err = conn.LRange("frontend:myothercname.com", 0, -1).Result()
	c.Assert(err, check.IsNil)
	c.Assert([]string{"myapp", addr.String()}, check.DeepEquals, cname)
}

func (s *S) TestUnsetCName(c *check.C) {
	router := hipacheRouter{prefix: "hipache"}
	err := router.AddBackend("myapp")
	c.Assert(err, check.IsNil)
	defer router.RemoveBackend("myapp")
	err = router.SetCName("myapp.com", "myapp")
	c.Assert(err, check.IsNil)
	conn, err := router.connect()
	c.Assert(err, check.IsNil)
	cnames, err := conn.LLen("cname:myapp").Result()
	c.Assert(err, check.IsNil)
	c.Assert(int64(1), check.Equals, cnames)
	err = router.UnsetCName("myapp.com", "myapp")
	c.Assert(err, check.IsNil)
	cnames, err = conn.LLen("cname:myapp").Result()
	c.Assert(err, check.IsNil)
	c.Assert(int64(0), check.Equals, cnames)
}

func (s *S) TestUnsetTwoCNames(c *check.C) {
	router := hipacheRouter{prefix: "hipache"}
	err := router.AddBackend("myapp")
	c.Assert(err, check.IsNil)
	defer router.RemoveBackend("myapp")
	err = router.SetCName("myapp.com", "myapp")
	c.Assert(err, check.IsNil)
	conn, err := router.connect()
	c.Assert(err, check.IsNil)
	cnames, err := conn.LLen("cname:myapp").Result()
	c.Assert(err, check.IsNil)
	c.Assert(int64(1), check.Equals, cnames)
	err = router.SetCName("myapptwo.com", "myapp")
	c.Assert(err, check.IsNil)
	cnames, err = conn.LLen("cname:myapp").Result()
	c.Assert(err, check.IsNil)
	c.Assert(int64(2), check.Equals, cnames)
	err = router.UnsetCName("myapp.com", "myapp")
	c.Assert(err, check.IsNil)
	cnames, err = conn.LLen("cname:myapp").Result()
	c.Assert(err, check.IsNil)
	c.Assert(int64(1), check.Equals, cnames)
	err = router.UnsetCName("myapptwo.com", "myapp")
	c.Assert(err, check.IsNil)
	cnames, err = conn.LLen("cname:myapp").Result()
	c.Assert(err, check.IsNil)
	c.Assert(int64(0), check.Equals, cnames)
}

func (s *S) TestAddr(c *check.C) {
	router := hipacheRouter{prefix: "hipache"}
	err := router.AddBackend("tip")
	c.Assert(err, check.IsNil)
	defer router.RemoveBackend("tip")
	url, _ := url.Parse("http://10.10.10.10")
	err = router.AddRoute("tip", url)
	c.Assert(err, check.IsNil)
	defer router.RemoveRoute("tip", url)
	addr, err := router.Addr("tip")
	c.Assert(err, check.IsNil)
	c.Assert(addr, check.Equals, "tip.golang.org")
}

func (s *S) TestAddrNoDomainConfigured(c *check.C) {
	r := hipacheRouter{prefix: "hipache"}
	err := r.AddBackend("tip")
	c.Assert(err, check.IsNil)
	defer r.RemoveBackend("tip")
	old, _ := config.Get("hipache:domain")
	defer config.Set("hipache:domain", old)
	config.Unset("hipache:domain")
	addr, err := r.Addr("tip")
	c.Assert(addr, check.Equals, "")
	e, ok := err.(*router.RouterError)
	c.Assert(ok, check.Equals, true)
	c.Assert(e.Op, check.Equals, "get")
}

func (s *S) TestAddrConnectFailure(c *check.C) {
	r := hipacheRouter{prefix: "hipache"}
	err := r.AddBackend("tip")
	c.Assert(err, check.IsNil)
	defer r.RemoveBackend("tip")
	config.Set("hipache:redis-server", "127.0.0.1:6380")
	defer config.Unset("hipache:redis-server")
	clearConnCache()
	r2 := hipacheRouter{prefix: "hipache"}
	addr, err := r2.Addr("tip")
	c.Assert(addr, check.Equals, "")
	e, ok := err.(*router.RouterError)
	c.Assert(ok, check.Equals, true)
	c.Assert(e.Op, check.Equals, "get")
}

func (s *S) TestRemoveElement(c *check.C) {
	router := hipacheRouter{prefix: "hipache"}
	count, err := router.removeElement("frontend:myapp.com", "10.10.10.10")
	c.Assert(err, check.IsNil)
	c.Assert(count, check.Equals, 0)
}

func (s *S) TestRoutes(c *check.C) {
	router := hipacheRouter{prefix: "hipache"}
	err := router.AddBackend("tip")
	c.Assert(err, check.IsNil)
	defer router.RemoveBackend("tip")
	addr, _ := url.Parse("http://10.10.10.10:8080")
	err = router.AddRoute("tip", addr)
	c.Assert(err, check.IsNil)
	defer router.RemoveRoute("tip", addr)
	routes, err := router.Routes("tip")
	c.Assert(err, check.IsNil)
	c.Assert(routes, check.DeepEquals, []*url.URL{addr})
}

func (s *S) TestSwap(c *check.C) {
	backend1 := "b1"
	backend2 := "b2"
	addr1, _ := url.Parse("http://127.0.0.1")
	addr2, _ := url.Parse("http://10.10.10.10")
	router := hipacheRouter{prefix: "hipache"}
	router.AddBackend(backend1)
	defer router.RemoveBackend(backend1)
	router.AddRoute(backend1, addr1)
	defer router.RemoveRoute(backend1, addr1)
	router.AddBackend(backend2)
	defer router.RemoveBackend(backend2)
	router.AddRoute(backend2, addr2)
	defer router.RemoveRoute(backend2, addr2)
	err := router.Swap(backend1, backend2, false)
	c.Assert(err, check.IsNil)
	conn, err := router.connect()
	c.Assert(err, check.IsNil)
	backend1Routes, err := conn.LRange("frontend:b2.golang.org", 0, -1).Result()
	c.Assert(err, check.IsNil)
	c.Assert([]string{"b2", addr1.String()}, check.DeepEquals, backend1Routes)
	backend2Routes, err := conn.LRange("frontend:b1.golang.org", 0, -1).Result()
	c.Assert(err, check.IsNil)
	c.Assert([]string{"b1", addr2.String()}, check.DeepEquals, backend2Routes)
}

func (s *S) TestAddRouteAfterCorruptedRedis(c *check.C) {
	backend1 := "b1"
	r := hipacheRouter{prefix: "hipache"}
	err := r.AddBackend(backend1)
	c.Assert(err, check.IsNil)
	redisConn, err := r.connect()
	c.Assert(err, check.IsNil)
	clearRedisKeys("frontend:*", redisConn, c)
	addr1, _ := url.Parse("http://127.0.0.1")
	err = r.AddRoute(backend1, addr1)
	c.Assert(err, check.Equals, router.ErrBackendNotFound)
}
