package fasthttp

import (
	"encoding/json"
	"fmt"
	"github.com/vela-security/vela-public/assert"
	"github.com/vela-security/vela-public/kind"
	"github.com/vela-security/vela-public/lua"
	"net"
	"strings"
)

type fsContext struct {
	sayJson *lua.LFunction
	say     *lua.LFunction
	append  *lua.LFunction
	exit    *lua.LFunction
	eof     *lua.LFunction
	rdt     *lua.LFunction //redirect
	rph     *lua.LFunction //request header
	rqh     *lua.LFunction
	try     *lua.LFunction
	bind    *lua.LFunction

	//meta lua.UserKV
}

func (fsc *fsContext) String() string                         { return fmt.Sprintf("fasthttp.context %p", fsc) }
func (fsc *fsContext) Type() lua.LValueType                   { return lua.LTObject }
func (fsc *fsContext) AssertFloat64() (float64, bool)         { return 0, false }
func (fsc *fsContext) AssertString() (string, bool)           { return "", false }
func (fsc *fsContext) AssertFunction() (*lua.LFunction, bool) { return nil, false }
func (fsc *fsContext) Peek() lua.LValue                       { return fsc }

func newContext() *fsContext {
	return &fsContext{
		sayJson: lua.NewFunction(sayJsonL),
		append:  lua.NewFunction(appendL),
		say:     lua.NewFunction(fsSay),
		exit:    lua.NewFunction(exitL),
		eof:     lua.NewFunction(eofL),
		rdt:     lua.NewFunction(fsRedirect),
		rph:     lua.NewFunction(rphL),
		rqh:     lua.NewFunction(rqhL),
		try:     lua.NewFunction(tryL),
		bind:    lua.NewFunction(luaBodyBind),
	}
}

func xPort(addr net.Addr) int {
	x, ok := addr.(*net.TCPAddr)
	if !ok {
		return 0
	}
	return x.Port
}

func regionCityId(ctx *RequestCtx) int {
	uv := ctx.UserValue("region_city")
	v, ok := uv.(int)
	if ok {
		return v
	}
	return 0
}

func regionRaw(ctx *RequestCtx) []byte {
	uv := ctx.UserValue("region")
	if uv == nil {
		return byteNull
	}

	v, ok := uv.(assert.IPv4Info)
	if ok {
		return v.Byte()
	}

	return byteNull
}

func fsSay(co *lua.LState) int {
	n := co.GetTop()
	if n == 0 {
		return 0
	}

	ctx := checkRequestCtx(co)
	data := make([]string, n)
	for i := 1; i <= n; i++ {
		data[i-1] = co.CheckString(i)
	}
	ctx.Response.SetBodyString(strings.Join(data,
		""))
	return 0
}

type ToJson interface {
	ToJson() ([]byte, error)
}

func sayJsonL(co *lua.LState) int {
	ctx := checkRequestCtx(co)
	lv := co.CheckAny(1)
	chunk, err := json.Marshal(lv)
	if err != nil {
		ctx.Error(err.Error(), 500)
		return 0
	}

	ctx.SetBody(chunk)
	return 0
}

func appendL(co *lua.LState) int {
	n := co.GetTop()
	if n == 0 {
		return 0
	}

	data := make([]string, n)
	ctx := checkRequestCtx(co)
	for i := 1; i <= n; i++ {
		data[i-1] = co.CheckString(i)
	}
	ctx.Response.AppendBody(lua.S2B(strings.Join(data, "")))
	return 0
}

func exitL(co *lua.LState) int {
	code := co.CheckInt(1)
	ctx := checkRequestCtx(co)
	ctx.Response.SetStatusCode(code)
	ctx.SetUserValue(eof_uv_key, true)
	return 0
}

func eofL(co *lua.LState) int {
	ctx := checkRequestCtx(co)
	ctx.SetUserValue(eof_uv_key, true)
	return 0
}

func tryL(co *lua.LState) int {
	n := co.GetTop()
	if n == 0 {
		co.RaiseError("invalid")
		return 0
	}

	data := make([]interface{}, n)
	format := make([]string, n)
	for i := 1; i <= n; i++ {
		format[i-1] = "%v "
		data[i-1] = co.CheckAny(i)
	}
	co.RaiseError(strings.Join(format, " "), data...)
	return 0
}

func fsHeaderHelper(co *lua.LState, resp bool) int {
	n := co.GetTop()
	if n == 0 {
		return 0
	}

	if n%2 != 0 {
		co.RaiseError("#args % 2 != 0")
		return 0
	}

	ctx := checkRequestCtx(co)

	for i := 0; i < n; {
		key := co.CheckString(i + 1)
		val := co.CheckString(i + 2)
		i += 2
		if resp {
			ctx.Response.Header.Set(key, val)
		} else {
			ctx.Request.Header.Set(key, val)
		}
	}

	return 0
}

func k2v(ctx *RequestCtx, key string) lua.LValue {
	switch key {
	//?????????
	case "host":
		return lua.B2L(ctx.Host())

	case "scheme":
		return lua.B2L(ctx.URI().Scheme())

	case "method":
		return lua.B2L(ctx.Method())

	//???????????????
	case "ua":
		return lua.B2L(ctx.UserAgent())

	//???????????????
	case "remote_addr":
		return lua.S2L(ctx.RemoteIP().String())
	case "remote_port":
		return lua.LInt(xPort(ctx.RemoteAddr()))

	//???????????????
	case "server_addr":
		return lua.S2L(ctx.LocalIP().String())
	case "server_port":
		return lua.LInt(xPort(ctx.LocalAddr()))

	case "time":
		return lua.S2L(ctx.Time().Format("2006-01-02 13:04:05.00"))

	//????????????
	case "uri":
		return lua.S2L(lua.B2S(ctx.URI().Path()))
	case "full_uri":
		return lua.S2L(ctx.URI().String())

	case "query":
		return lua.S2L(lua.B2S(ctx.URI().QueryString()))
	case "referer":
		return lua.S2L(lua.B2S(ctx.Request.Header.Peek("referer")))

	case "content_length":
		size := uint(ctx.Request.Header.ContentLength())
		return lua.LInt(size)

	case "size":
		raw := ctx.Request.Header.RawHeaders()
		full := ctx.URI().FullURI()
		return lua.LInt(len(raw) + len(full))

	case "content_type":
		return lua.S2L(lua.B2S(ctx.Request.Header.ContentType()))

	//????????????
	case "status":
		return lua.LInt(ctx.Response.StatusCode())
	case "sent":
		return lua.LInt(ctx.Response.Header.ContentLength())

	//?????????????????????
	case "region_raw":
		return lua.B2L(regionRaw(ctx))
	case "header_raw":
		return lua.B2L(ctx.Request.Header.RawHeaders())
	case "cookie_raw":
		return lua.B2L(ctx.Request.Header.Peek("cookie"))
	case "body_raw":
		return lua.B2L(ctx.Request.Body())

	default:
		switch {
		case strings.HasPrefix(key, "arg_"):
			return lua.B2L(ctx.QueryArgs().Peek(key[4:]))

		case strings.HasPrefix(key, "post_"):
			return lua.B2L(ctx.PostArgs().Peek(key[5:]))

		case strings.HasPrefix(key, "http_"):
			item := lua.S2B(key[5:])
			for i := 0; i < len(item); i++ {
				if item[i] == '_' {
					item[i] = '-'
				}
			}
			return lua.B2L(ctx.Request.Header.Peek(lua.B2S(item)))

		case strings.HasPrefix(key, "cookie_"):
			return lua.B2L(ctx.Request.Header.Cookie(key[7:]))

		case strings.HasPrefix(key, "region_"):
			uv := ctx.UserValue("region")
			if uv == nil {
				return lua.LNil
			}

			info, ok := uv.(assert.IPv4Info)
			if !ok {
				return lua.LNil
			}

			switch key[7:] {
			case "city":
				return lua.B2L(info.City())
			case "city_id":
				return lua.LInt(info.CityID())
			case "province":
				return lua.B2L(info.Province())
			case "region":
				return lua.B2L(info.Region())
			case "isp":
				return lua.B2L(info.ISP())
			default:
				return lua.LNil
			}

		case strings.HasPrefix(key, "param_"):
			uv := ctx.UserValue(key[6:])
			switch s := uv.(type) {
			case lua.LValue:
				return s
			case string:
				return lua.S2L(s)
			case int:
				return lua.LNumber(s)
			case interface{ String() string }:
				return lua.S2L(s.String())
			case interface{ Byte() []byte }:
				return lua.B2L(s.Byte())
			default:
				return lua.LNil
			}
		}
	}

	return lua.LNil
}

func luaBodyBind(L *lua.LState) int {
	ctx := checkRequestCtx(L)
	tn := L.CheckString(1)
	switch tn {
	case "json":
		obj, err := kind.NewFastJson(ctx.Request.Body())
		if err != nil {
			L.RaiseError("invalid json body")
			return 0
		}
		L.Push(L.NewAnyData(obj))
		return 1

	case "file":
		return newLuaFormFile(L)

	default:
		L.RaiseError("invalid bind type")
		return 0
	}
}
