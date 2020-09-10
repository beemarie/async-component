const express = require('express');
const Cloudant = require('@cloudant/cloudant');

const app = express();
app.use(express.json());
const apikey = process.env.APIKEY;
const acct = process.env.ACCOUNT;
const cloudant = new Cloudant({
  account: acct,
  plugins: {
    iamauth: {
      iamApiKey: apikey,
    },
  },
});

function sleep(ms) {
  const seconds = ms * 1000;
  return new Promise((resolve) => setTimeout(resolve, seconds));
}

app.get('/', async (req, res) => {
  const servRequestTime = Date.now();
  console.log('Hello world received a request.');
  const { duration } = req.query;
  const { reqNum } = req.query;
  await sleep(parseInt(duration, 10));
  cloudant.use('perf-test').insert({ time: servRequestTime }, reqNum).then((data) => {
    console.log(data);
  });
  res.send(`Hello, slept for ${duration} seconds`);
});

app.post('/testpost', async (req, res) => {
  // let servRequestTime = Date.now()
  const { body } = req;
  const duration = 1;
  console.log('Hello world received a request, with this body: ');
  console.log(body);
  await sleep(duration);
  res.send(`Hello, slept for ${duration} seconds with body: ${body}`);
});

const port = process.env.PORT || 8080;
app.listen(port, () => {
  console.log('Hello world listening on port', port);
});
