package com.demo.library.loan;

import jakarta.persistence.Entity;
import jakarta.persistence.GeneratedValue;
import jakarta.persistence.Id;
import java.time.LocalDate;

@Entity
public class Loan {
    @Id
    @GeneratedValue
    private Long id;
    private Long bookId;
    private Long memberId;
    private LocalDate dueOn;
    private boolean returned;

    protected Loan() {
    }

    public Loan(Long bookId, Long memberId, LocalDate dueOn) {
        this.bookId = bookId;
        this.memberId = memberId;
        this.dueOn = dueOn;
    }

    public Long getId() { return id; }
    public Long getBookId() { return bookId; }
    public Long getMemberId() { return memberId; }
    public LocalDate getDueOn() { return dueOn; }
    public boolean isReturned() { return returned; }
    public void markReturned() { this.returned = true; }
}
