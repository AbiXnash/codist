package com.demo.library.book;

import java.util.List;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/api/books")
public class BookController {
    private final BookService service;

    public BookController(BookService service) {
        this.service = service;
    }

    public record NewBook(String isbn, String title, int copies) {
    }

    @GetMapping
    public List<Book> all() {
        return service.all();
    }

    @PostMapping
    public Book add(@RequestBody NewBook book) {
        return service.add(book.isbn(), book.title(), book.copies());
    }
}
