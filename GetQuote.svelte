<form>
    <div>
        <input class="border py-1 px-2" id="symbol" name="symbol" type="text" size="10" placeholder="Enter Symbol" value="{symbol}">
        <button class="border rounded py-1 px-2 bg-gray-400" type="submit" on:click={getquote_click}>Get Quote</button>
    </div>
    <div>{@html profile}</div>
</form>

<script>
export let symbol = "";

let profile = "";

function getquote_click(e) {
    e.preventDefault();

    let quote = {};
    let inputsymbol = document.querySelector("#symbol");
    symbol = inputsymbol.value;

    let apikey = "G32E29AFMPQ2MCRG";
    let sreq = `https://www.alphavantage.co/query?function=GLOBAL_QUOTE&symbol=${symbol}&datatype=json&apikey=${apikey}`;
    let sreq2 = `https://www.alphavantage.co/query?function=OVERVIEW&symbol=${symbol}&datatype=json&apikey={apikey}`;

    fetch(sreq2, {method: "GET"})
    .then(res => res.json())
    .then(body => {
        console.log(body);
        quote.symbol = body["Symbol"];
        quote.name = body["Name"];
        console.log(`${quote.name} (${quote.symbol})`);

        return fetch(sreq, {method: "GET"});
    })
    .then(res => res.json())
    .then(body => {
        let q = body["Global Quote"];
        quote.price = q["05. price"];
        quote.change = q["09. change"];
        quote.changepct = q["10. change percent"];

        profile = `<p>${quote.name} (${quote.symbol})</p>`;
        profile += `<p>${quote.price} ${quote.change} (${quote.changepct})</p>`;
        console.log(body);
    })
    .catch(err => {
        console.log("Error received.");
        console.log(err);
    });
}
</script>
