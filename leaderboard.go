package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/redis/go-redis/v9"
	"gopkg.in/yaml.v3"
)

type Config struct {
	RedisURL string `yaml:"redisURL"`
}

// configure redis cache from config.yaml
func configureRedis() *string {
	data, read_err := os.ReadFile("./resources/config.yml")
	if read_err != nil {
		fmt.Print("Unable to read config.yml\n", read_err)
		return nil
	}
	config := Config{}
	parse_err := yaml.Unmarshal([]byte(data), &config)
	if parse_err != nil {
		fmt.Println("Unable to parse config.yml", parse_err)
		return nil
	}
	return &config.RedisURL
}

var Ctx = context.Background()
var RedisURL = *configureRedis()

// var opt, _ := redis.ParseURL(redisURL)
var Opt = redis.Options{Addr: "localhost:6379"}
var RedisClient = redis.NewClient(&Opt)

// pick a random key from map
func pick(m map[string]string, maxRand int) string {
	k := rand.Intn(maxRand)
	for key, _ := range m {
		if k == 0 {
			return key
		}
		k--
	}
	panic("unreachable")
}

// Checks if leaderboard has been updated by LSS:
// Checks 10 random UK people's rank in cache vs website,
// if any difference, its been updated
func leaderboardHasUpdated() bool {
	leaderboard, err := RedisClient.HGetAll(Ctx, "GB").Result()
	if err != nil {
		fmt.Println("checkIfUpdated: not able to get GB leaderboard from redis cache")
		return false
	}

	i := 0
	cachedRank := "0"
	foundDifference := false
	collector := colly.NewCollector()
	collector.AllowURLRevisit = true
	collector.OnHTML("div.block-table table tbody tr:nth-of-type(2)", func(row *colly.HTMLElement) {
		liveRank := row.ChildTexts("td")[0]
		if cachedRank != liveRank {
			foundDifference = true
			return
		}
	})
	for i < 10 {
		name := pick(leaderboard, 100)
		rank, err := RedisClient.HGet(Ctx, "GB", name).Result()
		cachedRank = rank
		if err != nil {
			fmt.Println("leadeboardHasUpdated: Not able to get " + name + "'s rank in redis cache")
			return false
		}
		nameQuery := strings.Join(strings.Split(name, " "), "+")
		fmt.Println("leadeboardHasUpdated: Checking live rank of " + nameQuery)
		collector.Visit("https://fabtcg.com/leaderboards/?query=" + nameQuery + "&mode=xp90&country=GB")

		if foundDifference {
			fmt.Println("leaderboardHasUpdated: difference found, cache needs updating")
			return true
		}
		i++
	}
	return false
}

// check if redis cache is empty (usually means something is wrong)
func cacheEmpty() bool {
	return len(RedisClient.HGetAll(Ctx, "GB").Val()) == 0
}

// Saves scraped data of leaderboard for a country in redis cache
func scrapeCallback(table *colly.HTMLElement, country string) {
	table.ForEach("tr", func(i int, row *colly.HTMLElement) {
		if i != 0 { // first row is header
			data := row.ChildTexts("td")
			nameOnly := strings.Split(data[1], " (")
			RedisClient.HSet(Ctx, country, nameOnly[0], data[0]) // name: rank
		}
	})

	fmt.Println("Caching complete for ", country)
	fmt.Println(RedisClient.HGetAll(Ctx, country))
}

func scrapeAllLeaderboards(countries []string, pages int, collector *colly.Collector) {
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

func main() {
	data, read_err := os.ReadFile("./resources/countries.yml")
	if read_err != nil {
		fmt.Println("Unable to read countries.yml\n", read_err)
	}
	var countries []string
	parse_err := yaml.Unmarshal([]byte(data), &countries)
	if parse_err != nil {
		fmt.Println("Unable to parse countries.yml", parse_err)
	}

	const pages = 3 // always visit the top 150 rank, unless US
	collector := colly.NewCollector()
	collector.AllowURLRevisit = true

	collector.OnRequest(func(r *colly.Request) {
		fmt.Println("Requesting ", r.URL)
	})
	collector.OnError((func(r *colly.Response, err error) {
		fmt.Println("Unable to request", r.Body)
	}))
	collector.OnHTML("div.block-table", func(h *colly.HTMLElement) {
		scrapeCallback(h, h.Request.URL.Query().Get("country"))
	})

	if cacheEmpty() {
		scrapeAllLeaderboards(countries, pages, collector)
	}

	tickChannel := time.Tick(1 * time.Hour)

	for next := range tickChannel {
		fmt.Println(next)
		if cacheEmpty() || leaderboardHasUpdated() {
			scrapeAllLeaderboards(countries, pages, collector)
		} else {
			fmt.Println("Not time to update")
		}

	}
}
