# gkbts #

Exports Slack GeekBot messages of work done into json.

## installation
```bash
$ go get -u github.com/akrulq/gkbts \
go build github.com/akrulq/gkbts
```

## usage
prerequisites: auth_cookie, token (obtain through slack web login from any request)
```
-auth_cookie string
    auth cookie value (d=<auth_cookie>)
-from string
    from date, example: 2021-01-31 (default <now -1 month>)
-token string
    auth token (default "xoxc-xxx")
-workspace string
    slack workspace

```

## examples
```bash
./gkbts --workspace=sampleworkspace --token="xoxc-..." --auth_cookie="OpPGMw%..."
```
export to csv with formatting
```bash
./gkbts --workspace=sampleworkspace --token="xoxc-..." --auth_cookie="OpPGMw%..." | jq -r '.[] | ["Musk, Elon", (.timestamp | fromdate | strftime("%d.%m.%Y")), "spacex.Sales", "Twitting", .spent_hrs, .text] | @csv'
```