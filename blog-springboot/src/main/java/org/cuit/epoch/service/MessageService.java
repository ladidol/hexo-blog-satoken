package org.cuit.epoch.service;

import com.baomidou.mybatisplus.extension.service.IService;
import org.cuit.epoch.dto.message.MessageBackDTO;
import org.cuit.epoch.dto.message.MessageDTO;
import org.cuit.epoch.entity.Message;
import org.cuit.epoch.vo.ConditionVO;
import org.cuit.epoch.vo.MessageVO;
import org.cuit.epoch.vo.ReviewVO;
import org.cuit.epoch.vo.page.PageResult;

import java.util.List;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/14 20:19
 * @description: {}
 */
public interface MessageService extends IService<Message> {

    /**
     * 添加留言弹幕
     *
     * @param messageVO 留言对象
     */
    void saveMessage(MessageVO messageVO);

    /**
     * 查看留言弹幕
     *
     * @return 留言列表
     */
    List<MessageDTO> listMessages();

    /**
     * 审核留言
     *
     * @param reviewVO 审查签证官
     */
    void updateMessagesReview(ReviewVO reviewVO);

    /**
     * 查看后台留言
     *
     * @param condition 条件
     * @return 留言列表
     */
    PageResult<MessageBackDTO> listMessageBackDTO(ConditionVO condition);

}