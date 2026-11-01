package com.demo.library.book;

import jakarta.persistence.Entity;
import jakarta.persistence.GeneratedValue;
import jakarta.persistence.Id;

@Entity
public class Book {
    @Id
    @GeneratedValue
    private Long id;
    private String isbn;
    private String title;
    private int copiesAvailable;

    protected Book() {
    }

    public Book(String isbn, String title, int copiesAvailable) {
        this.isbn = isbn;
        this.title = title;
        this.copiesAvailable = copiesAvailable;
    }

    public Long getId() { return id; }
    public String getIsbn() { return isbn; }
    public String getTitle() { return title; }
    public int getCopiesAvailable() { return copiesAvailable; }
    public void setCopiesAvailable(int copiesAvailable) { this.copiesAvailable = copiesAvailable; }
}
