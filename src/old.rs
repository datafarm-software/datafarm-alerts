use std::{
    io::{BufReader, prelude::*},
    net::{TcpListener, TcpStream},
};

type HandleFunc = fn(&[String]);

fn main() {
    let listener = TcpListener::bind("127.0.0.1:7878").unwrap();
    for stream in listener.incoming() {
        let stream = stream.unwrap();
        handle_connection(&stream);
    }
}

fn handle_connection(mut stream: &TcpStream) {
    let buf_reader = BufReader::new(stream);
    let http_request: Vec<String> = buf_reader
        .lines()
        .map(|result| result.unwrap())
        .take_while(|line| !line.is_empty())
        .collect();
    let Some(request_line) = http_request.first() else {
        println!("received empty http request");
        return;
    };
    let parts: Vec<&str> = request_line.split_whitespace().collect();
    assert!(
        parts.len() == 3,
        "first request line expected 3 parts, received {}",
        parts.len()
    );
    let (_, route, _) = (parts[0], parts[1], parts[2]);
    let Some(handle_func) = map_route_to_handle_func(route) else {
        println!("No handler for route: {route}");
        return;
    };
    handle_func(&http_request);
    let response = "HTTP/1.1 200 OK\r\n\r\n";
    stream.write_all(response.as_bytes()).unwrap();
}

fn map_route_to_handle_func(route: &str) -> Option<HandleFunc> {
    if route == "/notify" {
        Some(notify_func)
    } else {
        None
    }
}

fn notify_func(http_request: &[String]) {
    let req_bytes: Vec<u8> = http_request
        .iter()
        .flat_map(|s| s.clone().into_bytes())
        .collect();
    let x = str::from_utf8(&req_bytes).unwrap();
    println!("Request: {x}");
}
