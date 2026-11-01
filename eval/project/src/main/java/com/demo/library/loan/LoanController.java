package com.demo.library.loan;

import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/api/loans")
public class LoanController {
    private final LoanService service;

    public LoanController(LoanService service) {
        this.service = service;
    }

    public record Borrow(Long bookId, Long memberId) {
    }

    @PostMapping
    public Loan borrow(@RequestBody Borrow request) {
        return service.borrow(request.bookId(), request.memberId());
    }

    @PostMapping("/{id}/return")
    public void giveBack(@PathVariable Long id) {
        service.giveBack(id);
    }

    @PostMapping("/remind")
    public int remind() {
        return service.remindOverdue();
    }
}
