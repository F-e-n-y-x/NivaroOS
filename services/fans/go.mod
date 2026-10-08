module github.com/F-e-n-y-x/NivaroOS/services/fans

go 1.26.0

require github.com/F-e-n-y-x/NivaroOS/services/common v0.0.0

require (
	github.com/golang-jwt/jwt/v4 v4.5.2 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/labstack/echo-jwt/v5 v5.0.2 // indirect
	github.com/labstack/echo/v5 v5.4.0 // indirect
	github.com/tidwall/gjson v1.17.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.0 // indirect
	golang.org/x/time v0.15.0 // indirect
)

replace github.com/F-e-n-y-x/NivaroOS/services/common => ../common
