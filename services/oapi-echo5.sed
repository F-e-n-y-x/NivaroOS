# oapi-codegen v1.12.4 emits echo v4 server code; this rewrites it for
# echo v5 (go:generate pipes the generator output through it).
s#labstack/echo/v4#labstack/echo/v5#g
s/([^*A-Za-z0-9_])echo\.Context\b/\1*echo.Context/g
s/\*echo\.Route\b/echo.RouteInfo/g
