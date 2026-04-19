test url:
	curl -sS -X POST http://127.0.0.1:8080/scrape \
		-H "Content-Type: application/json" \
		-d '{"url":"{{url}}"}'
