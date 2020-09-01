import GetQuote from "./GetQuote.svelte";
const getquote = new GetQuote({
    target: document.querySelector("#getquote"),
    props: {
        symbol: "GLD",
    }
});

let apikey = "875d5614925e6d98037cbc8592b7bdc2";

// No need to specify exchange as it's auto-searched in marketstack.
//let exchange = "XNAS";
//let exchange = "ARCX";

//let symbol = "SBUX";
let symbol = "SGOL";
let symbol2 = "XAU";

// Looks like mutual funds not returned in marketstack.
//let symbol = "VFINX";
//let exchange = "NMFQS";

// No need to specify exchange as it's auto-searched in marketstack.
//let sreq = `http://api.marketstack.com/v1/tickers/${symbol}?exchange=${exchange}&access_key=${apikey}`;
//let sreq2 = `http://api.marketstack.com/v1/tickers/${symbol}/eod/latest?exchange=${exchange}&access_key=${apikey}`;

let sreq = `http://api.marketstack.com/v1/tickers/${symbol}?&access_key=${apikey}`;
let sreq2 = `http://api.marketstack.com/v1/tickers/${symbol}/eod/latest?&access_key=${apikey}`;
let sreq3 = `https://www.goldapi.io/api/${symbol2}`;

fetch(sreq, {method: "GET"})
.then(res => res.json())
.then(body => {
    let name = body["name"];
    symbol = body["symbol"];
    console.log(`symbol: ${symbol}`);
    console.log(`name: ${name}`);

    return fetch(sreq2, {method: "GET"});
})
.then(res => res.json())
.then(body => {
    let open = body["open"];
    let high = body["high"];
    let low = body["low"];
    let close = body["close"];
    let volume = body["volume"];
    console.log(`open: ${open}`);
    console.log(`close: ${close}`);
    console.log(`volume: ${volume}`);
});

let h = new Headers();
h.append("x-access-token", "goldapi-4g7vk3ykeikk2o0-io");
let req = new Request(sreq3, {
    method: "GET",
    headers: h,
});
fetch(req)
.then(res => res.json())
.then(body => {
    let price = body["price"];
    let currency = body["currency"];
    console.log(`price: ${price}`);
    console.log(`currency: ${currency}`);
});
