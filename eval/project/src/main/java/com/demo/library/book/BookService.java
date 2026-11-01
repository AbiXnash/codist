package com.demo.library.book;

import java.util.List;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class BookService {
    private final BookRepository books;

    public BookService(BookRepository books) {
        this.books = books;
    }

    @Transactional(readOnly = true)
    public List<Book> all() {
        return books.findAll();
    }

    @Transactional
    public Book add(String isbn, String title, int copies) {
        return books.save(new Book(isbn, title, copies));
    }
}
