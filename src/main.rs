use std::{
    io::{BufReader, prelude::*},
    net::{TcpListener, TcpStream},
};

fn main() {
    let listener = TcpListener::bind("127.0.0.1:7878").unwrap();
    for stream in listener.incoming() {
        let stream = stream.unwrap();
        handle_connection(&stream);
    }
}

fn handle_connection(stream: &TcpStream) {
    let buf_reader = BufReader::new(stream);
    let request_line = buf_reader.lines().next().unwrap().unwrap();
    let parts: Vec<&str> = request_line.split_whitespace().collect();
    assert!(
        (parts.len() == 3),
        "first request line had more than 3 spaces"
    );
    let (_, route, _) = (parts[0], parts[1], parts[2]);
    println!("Request route: {route}");
}
