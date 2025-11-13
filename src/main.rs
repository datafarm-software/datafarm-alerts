#![deny(warnings)]

use warp::Filter;

/// Provides a `RESTful` web server managing some Todos.
///
/// API will be:
///
/// - `GET /todos`: return a JSON list of Todos.
/// - `POST /todos`: create a new Todo.
/// - `PUT /todos/:id`: update a specific Todo.
/// - `DELETE /todos/:id`: delete a specific Todo.
#[tokio::main]
async fn main() {
    if env::var_os("RUST_LOG").is_none() {
        // Set `RUST_LOG=todos=debug` to see debug logs,
        // this only shows access logs.
        let _ = env::set_var("RUST_LOG", "todos=info");
    }
    pretty_env_logger::init();
    let api = filters::notify();
    // View access logs by setting `RUST_LOG=todos`.
    let routes = api.with(warp::log("todos"));
    // Start up the server...
    warp::serve(routes).run(([127, 0, 0, 1], 3030)).await;
}

mod filters {
    use super::handlers;
    use warp::Filter;

    pub fn notify() -> impl Filter<Extract = (impl warp::Reply,), Error = warp::Rejection> + Clone {
        warp::path!("notify")
            .and(warp::post())
            .and(json_body())
            .and_then(handlers::notify)
    }
    fn json_body() -> impl Filter<Extract = (String,), Error = warp::Rejection> + Clone {
        // When accepting a body, we want a JSON body
        // (and to reject huge payloads)...
        warp::body::content_length_limit(1024 * 16).and(warp::body::json())
    }
}

/// These are our API handlers, the ends of each filter chain.
/// Notice how thanks to using `Filter::and`, we can define a function
/// with the exact arguments we'd expect from each filter in the chain.
/// No tuples are needed, it's auto flattened for the functions.
mod handlers {
    use std::convert::Infallible;
    use warp::Reply;

    pub async fn notify(body: String) -> Result<warp::reply::Response, Infallible> {
        println!("Hello from the notify route!");
        println!("Body: {body:#?}");
        Ok(warp::reply::with_status("ok", warp::http::StatusCode::OK).into_response())
    }
}
