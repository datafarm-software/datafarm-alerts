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
    let http_request: Vec<_> = buf_reader
        .lines()
        .map(|result| result.unwrap())
        .take_while(|line| !line.is_empty())
        .collect();
    let request_line = &http_request[0];
    let parts: Vec<&str> = request_line.split_whitespace().collect();
    assert!(
        (parts.len() == 3),
        "first request line had more than 3 spaces"
    );
    let (_, route, _) = (parts[0], parts[1], parts[2]);
    let handle_func_opt = map_route_to_handle_func(route);
    if let Some(handle_func) = handle_func_opt {
        handle_func(&http_request);
    }
    let response = "HTTP/1.1 200 OK\r\n\r\n";
    stream.write_all(response.as_bytes()).unwrap();
}

fn map_route_to_handle_func(route: &str) -> Option<HandleFunc> {
    if route == "/notify" {
        Some(notify_func)
    } else {
        println!("Route {route} didn't match anything");
        None
    }
}

fn notify_func(http_request: &[String]) {
    println!("Request: {http_request:#?}");
}
