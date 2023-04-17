package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/redis/go-redis/v9"
	"gopkg.in/yaml.v3"
)

var ctx = context.Background()

func scrapeCallback(table *colly.HTMLElement, country string) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     "redis://default:fbc821c436db485b8e0a64517130a32a@fly-sultania.upstash.io", // "redis://default:fbc821c436db485b8e0a64517130a32a@fly-sultania.upstash.io"
		Password: "",
		DB:       0,
	})

	table.ForEach("tr", func(i int, row *colly.HTMLElement) {
		if i != 0 { // first row is header
			data := row.ChildTexts("td")
			rdb.HSet(ctx, country, data[1], data[0])
		}
	})

	fmt.Println("Caching complete for ", country)
	fmt.Println(rdb.HGetAll(ctx, country))
}

func main() {
	collector := colly.NewCollector()

	collector.OnRequest(func(r *colly.Request) {
		fmt.Println("Requesting ", r.URL)
	})
	collector.OnError((func(r *colly.Response, err error) {
		fmt.Println("Unable to request", r.Body)
	}))
	collector.OnHTML("div.block-table", func(h *colly.HTMLElement) {
		scrapeCallback(h, h.Request.URL.Query().Get("country"))
	})

	tickChannel := time.Tick(24 * time.Hour)
	const pages = 3 // always visit the top 150 rank, unless US

	for next := range tickChannel {
		fmt.Println(next)
		data, read_err := os.ReadFile("./countries.yml")
		if read_err != nil {
			fmt.Println("Unable to read conutries.yaml\n", read_err)
			continue
		}
		var countries []string
		parse_err := yaml.Unmarshal([]byte(data), &countries)
		if parse_err != nil {
			fmt.Println("Unable to parse countries.yaml", parse_err)
			continue
		}

		for _, country := range countries {
			fmt.Println(country)
			noOfPages := pages
			if country == "US" {
				noOfPages = 12 // US take the top 600
			}
			for i := 0; i < noOfPages; i++ {
				collector.Visit("https://fabtcg.com/leaderboards/?country=" + country + "&page=" + strconv.Itoa(i+1))
			}
		}

	}
}
