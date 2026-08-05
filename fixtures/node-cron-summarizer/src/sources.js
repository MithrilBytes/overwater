async function fetchOvernightArticles() {
  const response = await fetch(process.env.FEED_URL);
  const feed = await response.json();
  return feed.items.map((item) => item.title + "\n" + item.body);
}

async function postToSlack(text) {
  await fetch(process.env.SLACK_WEBHOOK_URL, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ text }),
  });
}

module.exports = { fetchOvernightArticles, postToSlack };
