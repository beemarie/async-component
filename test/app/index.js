const express = require('express');
const app = express();
app.use(express.json())
const Cloudant = require('@cloudant/cloudant');
const apikey = process.env.APIKEY
const acct = process.env.ACCOUNT
var cloudant = new Cloudant({
  account: acct,
  plugins: {
    iamauth: {
      iamApiKey: apikey
    }
  }
});

app.get('/', async (req, res) => {
  servRequestTime = Date.now()
  console.log('Hello world received a request.');
  duration = req.query.duration
  reqNum = req.query.reqNum
  await sleep(parseInt(duration))
  cloudant.use('perf-test').insert({ time: servRequestTime }, reqNum).then((data) => {
    console.log(data);
  });
  res.send(`Hello, slept for ${duration} seconds`);
});

app.post('/testpost', async (req, res) => {
  servRequestTime = Date.now()
  body = req.body
  duration = 1
  console.log('Hello world received a request, with this body: ');
  console.log(body)
  await sleep(duration)
  res.send(`Hello, slept for ${duration} seconds with body: ${body}`);
});

const port = process.env.PORT || 8080;
app.listen(port, () => {
  console.log('Hello world listening on port', port);
});

function sleep(ms) {
  seconds = ms*1000
  return new Promise(resolve => setTimeout(resolve, seconds));
}