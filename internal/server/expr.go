package server

import (
	"sync"

	"github.com/tidwall/expr"
	"github.com/tidwall/geojson"
	"github.com/tidwall/gjson"
	"github.com/tidwall/match"
	"github.com/tidwall/tile38/internal/field"
	"github.com/tidwall/tile38/internal/object"
)

type exprPool struct {
	pool *sync.Pool
}

func typeForObject(o *object.Object) expr.Value {
	switch o.Geo().(type) {
	case *geojson.Point, *geojson.SimplePoint:
		return expr.String("Point")
	case *geojson.LineString:
		return expr.String("LineString")
	case *geojson.Polygon, *geojson.Circle, *geojson.Rect:
		return expr.String("Polygon")
	case *geojson.MultiPoint:
		return expr.String("MultiPoint")
	case *geojson.MultiLineString:
		return expr.String("MultiLineString")
	case *geojson.MultiPolygon:
		return expr.String("MultiPolygon")
	case *geojson.GeometryCollection:
		return expr.String("GeometryCollection")
	case *geojson.Feature:
		return expr.String("Feature")
	case *geojson.FeatureCollection:
		return expr.String("FeatureCollection")
	default:
		return expr.Undefined
	}
}

func resultToValue(r gjson.Result) expr.Value {
	if !r.Exists() {
		return expr.Undefined
	}
	switch r.Type {
	case gjson.String:
		return expr.String(r.String())
	case gjson.False:
		return expr.Bool(false)
	case gjson.True:
		return expr.Bool(true)
	case gjson.Number:
		return expr.Number(r.Float())
	case gjson.JSON:
		return expr.Object(r)
	default:
		return expr.Null
	}
}

func newExprPool(s *Server) *exprPool {
	ext := expr.NewExtender(
		// ref
		func(info expr.RefInfo, ctx *expr.Context) (expr.Value, error) {
			o := ctx.UserData.(*object.Object)
			if !info.Chain {
				// root
				if r := gjson.Get(o.Geo().Members(), info.Ident); r.Exists() {
					return resultToValue(r), nil
				}
				switch info.Ident {
				case "id":
					return expr.String(o.ID()), nil
				case "type":
					return typeForObject(o), nil
				default:
					var rf field.Field
					var ok bool
					o.Fields().Scan(func(f field.Field) bool {
						if f.Name() == info.Ident {
							rf = f
							ok = true
							return false
						}
						return true
					})
					if ok {
						r := gjson.Parse(rf.Value().JSON())
						return resultToValue(r), nil
					}
				}
			} else {
				switch v := info.Value.Value().(type) {
				case gjson.Result:
					return resultToValue(v.Get(info.Ident)), nil
				default:
					// object methods
					switch info.Ident {
					case "match":
						return expr.Function("match"), nil
					}
				}
			}
			return expr.Undefined, nil
		},
		// call
		func(info expr.CallInfo, ctx *expr.Context) (expr.Value, error) {
			if info.Chain {
				switch info.Ident {
				case "match":
					t := match.Match(info.Value.String(), info.Args.String())
					return expr.Bool(t), nil
				}
			}
			return expr.Undefined, nil
		},
		// op
		func(info expr.OpInfo, ctx *expr.Context) (expr.Value, error) {
			// No custom operations
			return expr.Undefined, nil
		},
	)
	return &exprPool{
		pool: &sync.Pool{
			New: func() any {
				ctx := &expr.Context{
					Extender: ext,
				}
				return ctx
			},
		},
	}
}

func (p *exprPool) Get(o *object.Object) *expr.Context {
	ctx := p.pool.Get().(*expr.Context)
	ctx.UserData = o
	ctx.NoCase = true
	return ctx
}

func (p *exprPool) Put(ctx *expr.Context) {
	p.pool.Put(ctx)
}

func (where whereT) matchExpr(s *Server, o *object.Object) bool {
	ctx := s.epool.Get(o)
	res, _ := expr.Eval(where.name, ctx)
	s.epool.Put(ctx)
	return res.Bool()
}
